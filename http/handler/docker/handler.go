package docker

import (
	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/proxy"
	"github.com/gorilla/mux"
)

// Handler represents an HTTP API handler for proxying requests to the Docker API.
type Handler struct {
	*mux.Router
	dockerProxy    *proxy.LocalProxy
	clusterProxy   *proxy.ClusterProxy
	clusterService agent.ClusterService
	agentTags      map[string]string
}

// NewHandler returns a new instance of Handler.
// It sets the associated handle functions for all the Docker related HTTP endpoints.
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		dockerProxy:    proxy.NewLocalProxy(clusterService),
		clusterProxy:   proxy.NewClusterProxy(),
		clusterService: clusterService,
		agentTags:      agentTags,
	}

	h.PathPrefix("/").Handler(httperror.LoggerHandler(h.dockerOperation))
	return h
}
