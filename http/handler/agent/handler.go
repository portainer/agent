package agent

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
)

const (
	errAgentManagementDisabled = agent.Error("Agent management is disabled")
)

// Handler is the HTTP handler used to handle agent operations.
type Handler struct {
	*mux.Router
	clusterService agent.ClusterService
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the agent related HTTP endpoints.
func NewHandler(cs agent.ClusterService) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		clusterService: cs,
	}

	h.Handle("/agents",
		httperror.LoggerHandler(h.agentList)).Methods(http.MethodGet)

	return h
}
