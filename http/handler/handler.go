package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
)

type Handler struct {
	agentHandler       *AgentHandler
	dockerProxyHandler *DockerProxyHandler
}

func NewHandler(cs agent.ClusterService) *Handler {
	return &Handler{
		agentHandler:       NewAgentHandler(cs),
		dockerProxyHandler: NewDockerProxyHandler(cs),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(w, r)
	}
}

// encodeJSON encodes v to w in JSON format. WriteErrorResponse() is called if encoding fails.
func encodeJSON(w http.ResponseWriter, v interface{}, logger *log.Logger) {
	jsonData, err := json.Marshal(v)
	if err != nil {
		httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(jsonData)))

	_, err = w.Write(jsonData)
	if err != nil {
		httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, logger)
	}
}
