package handler

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/portainer/agent"
	httpagenthandler "github.com/portainer/agent/http/handler/agent"
	"github.com/portainer/agent/http/handler/browse"
	"github.com/portainer/agent/http/handler/docker"
	"github.com/portainer/agent/http/handler/host"
	"github.com/portainer/agent/http/handler/ping"
	"github.com/portainer/agent/http/handler/websocket"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
)

// Handler is the main handler of the application.
// Redirection to sub handlers is done in the ServeHTTP function.
type Handler struct {
	agentHandler       *httpagenthandler.Handler
	browseHandler      *browse.Handler
	browseHandlerV1    *browse.Handler
	dockerProxyHandler *docker.Handler
	webSocketHandler   *websocket.Handler
	hostHandler        *host.Handler
	pingHandler        *ping.Handler
}

// Config represents a server handler configuration
// used to create a new handler
type Config struct {
	SystemService    agent.SystemService
	ClusterService   agent.ClusterService
	SignatureService agent.DigitalSignatureService
	AgentTags        map[string]string
	AgentOptions     *agent.Options
	Secured          bool
}

var dockerAPIVersionRegexp = regexp.MustCompile(`(/v[0-9]\.[0-9]*)?`)

// NewHandler returns a pointer to a Handler.
func NewHandler(config *Config) *Handler {
	agentProxy := proxy.NewAgentProxy(config.ClusterService, config.AgentTags)
	notaryService := security.NewNotaryService(config.SignatureService, config.Secured)

	return &Handler{
		agentHandler:       httpagenthandler.NewHandler(config.ClusterService, notaryService),
		browseHandler:      browse.NewHandler(agentProxy, notaryService, config.AgentOptions),
		browseHandlerV1:    browse.NewHandlerV1(agentProxy, notaryService),
		dockerProxyHandler: docker.NewHandler(config.ClusterService, config.AgentTags, notaryService),
		webSocketHandler:   websocket.NewHandler(config.ClusterService, config.AgentTags, notaryService),
		hostHandler:        host.NewHandler(config.SystemService, agentProxy, notaryService),
		pingHandler:        ping.NewHandler(),
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	request.URL.Path = dockerAPIVersionRegexp.ReplaceAllString(request.URL.Path, "")
	rw.Header().Set(agent.HTTPResponseAgentHeaderName, agent.Version)
	rw.Header().Set(agent.HTTPResponseAgentApiVersion, agent.APIVersion)

	switch {
	case strings.HasPrefix(request.URL.Path, "/v1"):
		h.ServeHTTPV1(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v2"):
		h.ServeHTTPV2(rw, request)
	case strings.HasPrefix(request.URL.Path, "/ping"):
		h.pingHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/host"):
		h.hostHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/browse"):
		h.browseHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/websocket"):
		h.webSocketHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}
