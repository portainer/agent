package docker

import (
	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API handler for proxying requests to the Docker API.
type Handler struct {
	*mux.Router
	dockerProxy    *proxy.LocalProxy
	clusterProxy   *proxy.ClusterProxy
	clusterService agent.ClusterService
	agentTags      *agent.InfoTags
	useTLS         bool
}

// NewHandler returns a new instance of Handler.
// It sets the associated handle functions for all the Docker related HTTP endpoints.
func NewHandler(clusterService agent.ClusterService, agentTags *agent.InfoTags, notaryService *security.NotaryService, useTLS bool) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		dockerProxy:    proxy.NewLocalProxy(),
		clusterProxy:   proxy.NewClusterProxy(useTLS),
		clusterService: clusterService,
		agentTags:      agentTags,
		useTLS:         useTLS,
	}

	h.PathPrefix("/").Handler(notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.dockerOperation)))
	return h
}
