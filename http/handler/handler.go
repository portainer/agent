package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
)

type Handler struct {
	agentHandler  *AgentHandler
	dockerHandler *DockerHandler
}

func NewHandler(cs agent.ClusterService) *Handler {
	return &Handler{
		agentHandler:  NewAgentHandler(cs),
		dockerHandler: NewDockerHandler(cs),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/"):
		h.dockerHandler.ServeHTTP(w, r)
	}
}

// encodeJSON encodes v to w in JSON format. WriteErrorResponse() is called if encoding fails.
func encodeJSON(w http.ResponseWriter, v interface{}, logger *log.Logger) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, logger)
	}
}
