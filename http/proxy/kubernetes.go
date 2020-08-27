package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"
)

const kubernetesAPIURL = "https://kubernetes.default.svc"

func NewKubernetesProxy() http.Handler {
	remoteURL, _ := url.Parse(kubernetesAPIURL)
	proxy := httputil.NewSingleHostReverseProxy(remoteURL)

	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	return proxy
}
