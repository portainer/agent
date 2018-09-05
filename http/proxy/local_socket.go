// +build !windows

package proxy

import (
	"net"
	"net/http"
)

// NewLocalProxy returns a pointer to a LocalProxy.
func NewLocalProxy() *LocalProxy {
	proxy := &LocalProxy{
		transport: newSocketTransport("/var/run/docker.sock"),
	}
	return proxy
}

func newSocketTransport(socketPath string) *http.Transport {
	return &http.Transport{
		Dial: func(proto, addr string) (conn net.Conn, err error) {
			return net.Dial("unix", socketPath)
		},
	}
}
