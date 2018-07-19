package handler

import (
	"net/http"
	"regexp"
	"strings"

	"bitbucket.org/portainer/agent"
	httpagenthandler "bitbucket.org/portainer/agent/http/handler/agent"
	"bitbucket.org/portainer/agent/http/handler/browse"
	"bitbucket.org/portainer/agent/http/handler/docker"
	"bitbucket.org/portainer/agent/http/handler/websocket"
)

// Handler is the main handler of the application.
// Redirection to sub handlers is done in the ServeHTTP function.
type Handler struct {
	agentHandler       *httpagenthandler.Handler
	browseHandler      *browse.Handler
	dockerProxyHandler *docker.Handler
	webSocketHandler   *websocket.Handler
}

const (
	errInvalidQueryParameters = agent.Error("Invalid query parameters")
)

var apiVersionRe = regexp.MustCompile(`(/v[0-9]\.[0-9]*)?`)

// NewHandler returns a pointer to a Handler.
func NewHandler(cs agent.ClusterService, agentTags map[string]string) *Handler {
	return &Handler{
		agentHandler:       httpagenthandler.NewHandler(cs),
		browseHandler:      browse.NewHandler(cs, agentTags),
		dockerProxyHandler: docker.NewHandler(cs, agentTags),
		webSocketHandler:   websocket.NewHandler(cs, agentTags),
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	switch {
	case strings.HasPrefix(request.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/browse"):
		h.browseHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/websocket"):
		h.webSocketHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		request.URL.Path = apiVersionRe.ReplaceAllString(request.URL.Path, "")
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}
