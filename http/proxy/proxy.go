package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"bitbucket.org/portainer/agent"
	"github.com/koding/websocketproxy"
)

func ProxyOperation(rw http.ResponseWriter, request *http.Request, target *agent.ClusterMember) {
	url := request.URL
	url.Host = target.IPAddress + ":" + target.Port

	// TODO: agents will use TLS as default protocol, might need to default to https
	url.Scheme = "http"

	proxyHTTPRequest(rw, request, url, target.NodeName)
}

func ProxyWebsocketOperation(rw http.ResponseWriter, request *http.Request, target *agent.ClusterMember) {
	url := request.URL
	url.Host = target.IPAddress + ":" + target.Port

	// TODO: agents will use TLS as default protocol, might need to switch to wss
	url.Scheme = "ws"

	proxyWebsocketRequest(rw, request, url, target.NodeName)
}

func proxyHTTPRequest(rw http.ResponseWriter, request *http.Request, target *url.URL, targetNode string) {
	proxy := newSingleHostReverseProxyWithAgentHeader(target, targetNode)
	proxy.ServeHTTP(rw, request)
}

func proxyWebsocketRequest(rw http.ResponseWriter, request *http.Request, target *url.URL, targetNode string) {
	proxy := websocketproxy.NewProxy(target)
	proxy.Director = func(incoming *http.Request, out http.Header) {
		out.Set(agent.HTTPTargetHeaderName, targetNode)
	}
	proxy.ServeHTTP(rw, request)
}

func newSingleHostReverseProxyWithAgentHeader(target *url.URL, targetNode string) *httputil.ReverseProxy {
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
	}
}
