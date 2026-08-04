package proxy

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/portainer/portainer/api/crypto"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const kubernetesAPIURL = "https://kubernetes.default.svc"

// http1ProtoName is the ALPN identifier for HTTP/1.1.
const http1ProtoName = "http/1.1"

// forceHTTP1 constrains the transport built from config to HTTP/1.1.
//
// rest.TransportFor enables HTTP/2 by default, but Go's HTTP/2 client rejects
// requests carrying an Upgrade header. That breaks the SPDY-based upgrade verbs
// (kubectl port-forward/exec/attach) once traffic crosses this proxy hop.
// Pinning NextProtos to http/1.1 keeps those upgrades working while retaining
// the in-cluster credentials.
func forceHTTP1(config *rest.Config) {
	config.TLSClientConfig.NextProtos = []string{http1ProtoName}
}

func NewKubernetesProxy() *httputil.ReverseProxy {
	remoteURL, _ := url.Parse(kubernetesAPIURL)

	// This preserves the behaviour of httputil.NewSingleHostReverseProxy but using the Rewrite field
	// instead of the deprecated Director field.
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(remoteURL)
			r.Out.Host = r.In.Host
		},
	}
	proxy.Transport = &http.Transport{
		TLSClientConfig: crypto.CreateTLSConfiguration(true),
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		if errors.Is(err, rest.ErrNotInCluster) {
			log.Info().Msg("kubernetes InClusterConfig reports not running in cluster")
		} else {
			log.Error().Err(err).Msg("error getting in-cluster config for Kubernetes proxy")
		}
	}

	var transport http.RoundTripper
	if config != nil {
		forceHTTP1(config)
		transport, err = rest.TransportFor(config)
		if err != nil {
			log.Error().Err(err).Msg("error getting transport for Kubernetes proxy with in-cluster config")
		}
	}

	if transport != nil {
		log.Info().
			Bool("insecure", config.TLSClientConfig.Insecure).
			Bool("can_tls_skip_verify", fips.CanTLSSkipVerify()).
			Msg("using in-cluster transport config for Kubernetes proxy")

		proxy.Transport = transport
	} else {
		log.Info().Msg("using default transport for Kubernetes proxy")
		proxy.Transport = &http.Transport{
			TLSClientConfig: crypto.CreateTLSConfiguration(true),
		}
	}

	kubeconfigPath := os.Getenv("DEV_KUBECONFIG_PATH")
	if kubeconfigPath == "" {
		return proxy
	}
	// Developers can set the DEV_KUBECONFIG_PATH to run agent locally
	// instead of in-cluster
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		log.Error().Err(err).Msg("error building config from kubeconfig file")
		return proxy
	}

	if config == nil {
		log.Error().Msg("kubeconfig resulted in nil config")
		return proxy
	}

	remoteURL, err = url.Parse(config.Host)
	if err != nil {
		log.Error().Err(err).Msg("error parsing kubernetes host URL")
		return proxy
	}

	forceHTTP1(config)
	transport, err = rest.TransportFor(config)
	if err != nil {
		log.Error().Err(err).Msg("error getting transport for dev kubeconfig")
		return proxy
	}

	proxy = httputil.NewSingleHostReverseProxy(remoteURL)
	proxy.Transport = transport

	return proxy
}
