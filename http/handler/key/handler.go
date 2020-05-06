package key

import (
	"net/http"

	"github.com/portainer/agent"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/security"
	"github.com/portainer/agent/internal/edge"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle Edge key operations.
type Handler struct {
	*mux.Router
	tunnelOperator agent.TunnelOperator
	edgeManager    *edge.Manager
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Edge key related HTTP endpoints.
// This handler is meant to be used when the agent is started in Edge mode, all the API endpoints will return
// a HTTP 503 service not available if edge mode is disabled.
func NewHandler(tunnelOperator agent.TunnelOperator, notaryService *security.NotaryService, edgeManager *edge.Manager) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		edgeManager:    edgeManager,
		tunnelOperator: tunnelOperator,
	}

	h.Handle("/key",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.keyInspect))).Methods(http.MethodGet)
	h.Handle("/key",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.keyCreate))).Methods(http.MethodPost)

	return h
}
