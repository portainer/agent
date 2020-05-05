package key

import (
	"net/http"

	"github.com/portainer/agent"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle Edge key operations.
type Handler struct {
	*mux.Router
	tunnelOperator agent.TunnelOperator
	edgeManager    agent.EdgeManager
	clusterService agent.ClusterService
	edgeMode       bool
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Edge key related HTTP endpoints.
// This handler is meant to be used when the agent is started in Edge mode, all the API endpoints will return
// a HTTP 503 service not available if edge mode is disabled.
func NewHandler(tunnelOperator agent.TunnelOperator, clusterService agent.ClusterService, notaryService *security.NotaryService, edgeManager agent.EdgeManager, edgeMode bool) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		edgeManager:    edgeManager,
		tunnelOperator: tunnelOperator,
		clusterService: clusterService,
		edgeMode:       edgeMode,
	}

	h.Handle("/key",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.keyInspect))).Methods(http.MethodGet)
	h.Handle("/key",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.keyCreate))).Methods(http.MethodPost)

	return h
}
