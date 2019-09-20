package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/koding/websocketproxy"
	"github.com/portainer/agent"
)

// AgentHTTPRequest redirects a HTTP request to another agent.
func AgentHTTPRequest(rw http.ResponseWriter, request *http.Request, target *agent.ClusterMember, useTLS bool) {
	urlCopy := request.URL
	urlCopy.Host = target.IPAddress + ":" + target.Port

	urlCopy.Scheme = "http"
	if useTLS {
		urlCopy.Scheme = "https"
	}

	proxyHTTPRequest(rw, request, urlCopy, target.NodeName)
}

// WebsocketRequest redirects a websocket request to another agent.
func WebsocketRequest(rw http.ResponseWriter, request *http.Request, target *agent.ClusterMember) {
	urlCopy := request.URL
	urlCopy.Host = target.IPAddress + ":" + target.Port

	urlCopy.Scheme = "ws"
	if request.TLS != nil {
		urlCopy.Scheme = "wss"
	}

	proxyWebsocketRequest(rw, request, urlCopy, target.NodeName)
}

func proxyHTTPRequest(rw http.ResponseWriter, request *http.Request, target *url.URL, targetNode string) {
	proxy := newAgentReverseProxy(target, targetNode)
	proxy.ServeHTTP(rw, request)
}

func proxyWebsocketRequest(rw http.ResponseWriter, request *http.Request, target *url.URL, targetNode string) {
	proxy := websocketproxy.NewProxy(target)
	proxy.Director = func(incoming *http.Request, out http.Header) {
		out.Set(agent.HTTPSignatureHeaderName, request.Header.Get(agent.HTTPSignatureHeaderName))
		out.Set(agent.HTTPPublicKeyHeaderName, request.Header.Get(agent.HTTPPublicKeyHeaderName))
		out.Set(agent.HTTPTargetHeaderName, targetNode)
	}
	proxy.Dialer = &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	proxy.ServeHTTP(rw, request)
}

func newAgentReverseProxy(target *url.URL, targetNode string) *httputil.ReverseProxy {
	targetQuery := target.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = target.Path
		req.Host = req.URL.Host
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		req.Header.Set(agent.HTTPTargetHeaderName, targetNode)
	}

	return &httputil.ReverseProxy{
		Director: director,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
}
