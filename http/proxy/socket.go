package proxy

import (
	"io"
	"net"
	"net/http"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
)

// SocketProxy is a service used to proxy requests to a Unix socket.
// The proxy operation implementation is defined in the ServeHTTP funtion..
type SocketProxy struct {
	transport *http.Transport
}

// NewSocketProxy returns a pointer to a SocketProxy.
func NewSocketProxy(socketPath string, clusterService agent.ClusterService) *SocketProxy {
	proxy := &SocketProxy{
		transport: newSocketTransport(socketPath),
	}
	return proxy
}

func (proxy *SocketProxy) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	request.URL.Scheme = "http"
	request.URL.Host = "unixsocket"

	res, err := proxy.transport.RoundTrip(request)
	if err != nil {
		code := http.StatusInternalServerError
		if res != nil && res.StatusCode != 0 {
			code = res.StatusCode
		}
		httperror.WriteError(rw, code, "Unable to proxy the request via the Docker socket", err)
		return
	}

	defer res.Body.Close()

	for k, vv := range res.Header {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}
	rw.Header().Set(agent.HTTPResponseAgentHeaderName, agent.AgentVersion)

	rw.WriteHeader(res.StatusCode)

	// TODO: resource duplication error: it seems that the body size is different here
	// from the size retrieve in cluster.go
	io.Copy(rw, res.Body)
}

func newSocketTransport(socketPath string) *http.Transport {
	return &http.Transport{
		Dial: func(proto, addr string) (conn net.Conn, err error) {
			return net.Dial("unix", socketPath)
		},
	}
}
