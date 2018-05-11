package proxy

import (
	"io"
	"log"
	"net"
	"net/http"
	"os"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
)

// SocketProxy is a service used to proxy requests to a Unix socket.
// The proxy operation implementation is defined in the ServeHTTP funtion..
type SocketProxy struct {
	transport *http.Transport
	logger    *log.Logger
}

// NewSocketProxy returns a pointer to a SocketProxy.
func NewSocketProxy(socketPath string, clusterService agent.ClusterService) *SocketProxy {
	proxy := &SocketProxy{
		transport: newSocketTransport(socketPath),
		logger:    log.New(os.Stderr, "", log.LstdFlags),
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
		httperror.WriteErrorResponse(rw, err, code, proxy.logger)
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
	io.Copy(rw, res.Body)
}

func newSocketTransport(socketPath string) *http.Transport {
	return &http.Transport{
		Dial: func(proto, addr string) (conn net.Conn, err error) {
			return net.Dial("unix", socketPath)
		},
	}
}
