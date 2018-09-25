package browse

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler/agentproxy"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle volume browsing operations.
type Handler struct {
	agentproxy.Handler
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Browse related HTTP endpoints.
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string) *Handler {
	h := &Handler{}
	h.CreateAgentProxyHandler(clusterService, agentTags)

	h.Handle("/browse/{id}/ls",
		h.AgentProxy(httperror.LoggerHandler(h.browseList))).Methods(http.MethodGet)
	h.Handle("/browse/{id}/get",
		h.AgentProxy(httperror.LoggerHandler(h.browseGet))).Methods(http.MethodGet)
	h.Handle("/browse/{id}/delete",
		h.AgentProxy(httperror.LoggerHandler(h.browseDelete))).Methods(http.MethodDelete)
	h.Handle("/browse/{id}/rename",
		h.AgentProxy(httperror.LoggerHandler(h.browseRename))).Methods(http.MethodPut)
	h.Handle("/browse/{id}/put",
		h.AgentProxy(httperror.LoggerHandler(h.browsePut))).Methods(http.MethodPost)
	return h
}
