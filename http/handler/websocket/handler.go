package websocket

import (
	"github.com/portainer/agent"
	httperror "github.com/portainer/agent/http/error"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
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
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string) *Handler {
	h := &Handler{
		Router:             mux.NewRouter(),
		connectionUpgrader: websocket.Upgrader{},
		clusterService:     clusterService,
		agentTags:          agentTags,
	}

	h.Handle("/websocket/exec", httperror.LoggerHandler(h.websocketExec))
	return h
}
