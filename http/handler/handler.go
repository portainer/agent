package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
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

var apiVersionRe = regexp.MustCompile(`(/v[0-9]\.[0-9]*)?`)

func NewHandler(cs agent.ClusterService, agentTags map[string]string) *Handler {
	return &Handler{
		agentHandler:       NewAgentHandler(cs),
		dockerProxyHandler: NewDockerProxyHandler(cs, agentTags),
		webSocketHandler:   NewWebSocketHandler(cs, agentTags),
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	switch {
	case strings.HasPrefix(request.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/websocket"):
		h.webSocketHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		request.URL.Path = apiVersionRe.ReplaceAllString(request.URL.Path, "")
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}

func writeJSONResponse(rw http.ResponseWriter, data interface{}, logger *log.Logger) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		httperror.WriteErrorResponse(rw, err, http.StatusInternalServerError, logger)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Content-Length", strconv.Itoa(len(jsonData)))
	rw.Header().Set(agent.HTTPResponseAgentHeaderName, agent.AgentVersion)

	_, err = rw.Write(jsonData)
	if err != nil {
		httperror.WriteErrorResponse(rw, err, http.StatusInternalServerError, logger)
	}
}
