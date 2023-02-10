package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/portainer/agent/crypto"
)

const kubernetesAPIURL = "https://kubernetes.default.svc"

func NewKubernetesProxy() http.Handler {
	remoteURL, _ := url.Parse(kubernetesAPIURL)
	proxy := httputil.NewSingleHostReverseProxy(remoteURL)

	tlsConfig := crypto.CreateTLSConfiguration()
	tlsConfig.InsecureSkipVerify = true

	proxy.Transport = &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return proxy
}
