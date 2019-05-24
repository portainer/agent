package websocket

import (
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

type (
	// Handler represents an HTTP API handler for proxying requests to a web socket.
	Handler struct {
		*mux.Router
		clusterService     agent.ClusterService
		connectionUpgrader websocket.Upgrader
		agentTags          map[string]string
	}

	execStartOperationPayload struct {
		Tty    bool
		Detach bool
	}
)

// NewHandler returns a new instance of Handler.
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string, notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router:             mux.NewRouter(),
		connectionUpgrader: websocket.Upgrader{},
		clusterService:     clusterService,
		agentTags:          agentTags,
	}

	h.Handle("/websocket/attach", notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.websocketAttach)))
	h.Handle("/websocket/exec", notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.websocketExec)))
	return h
}
