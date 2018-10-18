package docker

import (
	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/libhttp/error"
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
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string, notaryService agent.NotaryService) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		dockerProxy:    proxy.NewLocalProxy(),
		clusterProxy:   proxy.NewClusterProxy(),
		clusterService: clusterService,
		agentTags:      agentTags,
	}

	h.PathPrefix("/").Handler(notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.dockerOperation)))
	return h
}
