package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func NewNomadProxy(nomadAddr string) http.Handler {
	remoteURL, _ := url.Parse(nomadAddr)
	proxy := httputil.NewSingleHostReverseProxy(remoteURL)

	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	return proxy
}
