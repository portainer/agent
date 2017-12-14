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

type SocketProxy struct {
	transport *http.Transport
	logger    *log.Logger
}

func NewSocketProxy(socketPath string, clusterService agent.ClusterService) *SocketProxy {
	proxy := &SocketProxy{
		transport: newSocketTransport(socketPath),
		logger:    log.New(os.Stderr, "", log.LstdFlags),
	}
	return proxy
}

func (proxy *SocketProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Force URL/domain to http/unixsocket to be able to
	// use http.Transport RoundTrip to do the requests via the socket
	r.URL.Scheme = "http"
	r.URL.Host = "unixsocket"

	res, err := proxy.transport.RoundTrip(r)
	if err != nil {
		code := http.StatusInternalServerError
		if res != nil && res.StatusCode != 0 {
			code = res.StatusCode
		}
		httperror.WriteErrorResponse(w, err, code, proxy.logger)
		return
	}
	defer res.Body.Close()

	for k, vv := range res.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(res.StatusCode)

	if _, err := io.Copy(w, res.Body); err != nil {
		httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, proxy.logger)
	}
}

func newSocketTransport(socketPath string) *http.Transport {
	return &http.Transport{
		Dial: func(proto, addr string) (conn net.Conn, err error) {
			return net.Dial("unix", socketPath)
		},
	}
}
