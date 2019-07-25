package agent

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle agent operations.
type Handler struct {
	*mux.Router
	clusterService agent.ClusterService
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the agent related HTTP endpoints.
func NewHandler(cs agent.ClusterService, notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		clusterService: cs,
	}

	h.Handle("/agents",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.agentList))).Methods(http.MethodGet)

	return h
}
