package proxy

import (
	"net/http"
	"strings"

	"bitbucket.org/portainer/agent"
)

type (
	dockerProxyTransport struct {
		transport      *http.Transport
		clusterService agent.ClusterService
	}
)

func (p *dockerProxyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return p.proxyDockerRequest(request)
}

func (p *dockerProxyTransport) proxyDockerRequest(request *http.Request) (*http.Response, error) {
	path := request.URL.Path

	switch {
	case strings.HasPrefix(path, "/containers/json"):
		return p.aggregationOperation(request)
	default:
		return p.executeDockerRequest(request)
	}
}

func (p *dockerProxyTransport) aggregationOperation(request *http.Request) (*http.Response, error) {

	clusterMembers, err := p.clusterService.Members()
	if err != nil {
		return nil, err
	}

	return p.transport.RoundTrip(request)
}

func (p *dockerProxyTransport) executeDockerRequest(request *http.Request) (*http.Response, error) {
	return p.transport.RoundTrip(request)
}
