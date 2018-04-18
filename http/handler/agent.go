package handler

import (
	"log"
	"net/http"
	"os"

	"bitbucket.org/portainer/agent"
	"github.com/gorilla/mux"
)

// AgentHandler is a handler used to managed requests on /agents
type AgentHandler struct {
	*mux.Router
	logger         *log.Logger
	clusterService agent.ClusterService
}

// NewAgentHandler returns a pointer to an AgentHandler
// It sets the associated handle functions for all the agent related HTTP endpoints.
func NewAgentHandler(cs agent.ClusterService) *AgentHandler {
	h := &AgentHandler{
		Router:         mux.NewRouter(),
		logger:         log.New(os.Stderr, "", log.LstdFlags),
		clusterService: cs,
	}

	h.Handle("/agents",
		http.HandlerFunc(h.handleGetAgents)).Methods(http.MethodGet)

	return h
}

func (handler *AgentHandler) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	members := handler.clusterService.Members()
	writeJSONResponse(w, members, handler.logger)
}
