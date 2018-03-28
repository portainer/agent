package handler

import (
	"encoding/json"
	"io"
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
	webSocketHandler   *WebSocketHandler
}

const (
	errInvalidQueryParameters = agent.Error("Invalid query parameters")
)

func NewHandler(cs agent.ClusterService) *Handler {
	return &Handler{
		agentHandler:       NewAgentHandler(cs),
		dockerProxyHandler: NewDockerProxyHandler(cs),
		webSocketHandler:   NewWebSocketHandler(cs),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/websocket"):
		h.webSocketHandler.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(w, r)
	}
}

func encodeJSON(w http.ResponseWriter, v interface{}, logger *log.Logger) {
	if v == nil {
		return
	}

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

func copyResponseToResponseWriter(response *http.Response, responseWriter http.ResponseWriter) error {

	defer response.Body.Close()

	for k, vv := range response.Header {
		for _, v := range vv {
			responseWriter.Header().Add(k, v)
		}
	}

	responseWriter.WriteHeader(response.StatusCode)

	_, err := io.Copy(responseWriter, response.Body)
	if err != nil {
		return err
	}

	return nil
}
