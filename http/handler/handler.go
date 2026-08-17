package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge"
	"github.com/portainer/agent/exec"
	httpagenthandler "github.com/portainer/agent/http/handler/agent"
	"github.com/portainer/agent/http/handler/browse"
	"github.com/portainer/agent/http/handler/diagnostics"
	"github.com/portainer/agent/http/handler/docker"
	"github.com/portainer/agent/http/handler/dockerhub"
	"github.com/portainer/agent/http/handler/host"
	"github.com/portainer/agent/http/handler/key"
	"github.com/portainer/agent/http/handler/kubernetes"
	"github.com/portainer/agent/http/handler/kubernetesproxy"
	agentmetrics "github.com/portainer/agent/http/handler/metrics"
	"github.com/portainer/agent/http/handler/ping"
	"github.com/portainer/agent/http/handler/websocket"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	kubecli "github.com/portainer/agent/kubernetes"
	"github.com/portainer/portainer/pkg/libhttp"
)

// Handler is the main handler of the application.
// Redirection to sub handlers is done in the ServeHTTP function.
type Handler struct {
	agentHandler           *httpagenthandler.Handler
	browseHandler          *browse.Handler
	browseHandlerV1        *browse.Handler
	dockerProxyHandler     *docker.Handler
	dockerhubHandler       *dockerhub.Handler
	keyHandler             *key.Handler
	kubernetesHandler      *kubernetes.Handler
	kubernetesProxyHandler *kubernetesproxy.Handler
	webSocketHandler       *websocket.Handler
	hostHandler            *host.Handler
	pingHandler            *ping.Handler
	diagnosticsHandler     *diagnostics.Handler
	metricsHandler         *agentmetrics.Handler
	containerPlatform      agent.ContainerPlatform
}

// Config represents a server handler configuration
// used to create a new handler
type Config struct {
	SystemService          agent.SystemService
	ClusterService         agent.ClusterService
	SignatureService       agent.DigitalSignatureService
	KubeClient             *kubecli.KubeClient
	KubernetesDeployer     *exec.KubernetesDeployer
	EdgeManager            *edge.Manager
	RuntimeConfiguration   *agent.RuntimeConfig
	UseTLS                 bool
	ContainerPlatform      agent.ContainerPlatform
	PullLimitCheckDisabled bool
}

// NewHandler returns a pointer to a Handler.
func NewHandler(config *Config) *Handler {
	agentProxy := proxy.NewAgentProxy(config.ClusterService, config.RuntimeConfiguration, config.UseTLS)
	notaryService := security.NewNotaryService(config.SignatureService, true)

	return &Handler{
		agentHandler:           httpagenthandler.NewHandler(config.ClusterService, notaryService),
		browseHandler:          browse.NewHandler(agentProxy, notaryService),
		browseHandlerV1:        browse.NewHandlerV1(agentProxy, notaryService),
		dockerProxyHandler:     docker.NewHandler(config.ClusterService, config.RuntimeConfiguration, notaryService, config.UseTLS),
		dockerhubHandler:       dockerhub.NewHandler(notaryService, config.PullLimitCheckDisabled),
		diagnosticsHandler:     diagnostics.NewHandler(config.ContainerPlatform, config.EdgeManager, notaryService),
		metricsHandler:         resolveMetricsHandler(config.EdgeManager),
		keyHandler:             key.NewHandler(notaryService, config.EdgeManager),
		kubernetesHandler:      kubernetes.NewHandler(notaryService, config.KubernetesDeployer),
		kubernetesProxyHandler: kubernetesproxy.NewHandler(notaryService),
		webSocketHandler:       websocket.NewHandler(config.ClusterService, config.RuntimeConfiguration, notaryService, config.KubeClient),
		hostHandler:            host.NewHandler(config.SystemService, agentProxy, notaryService),
		pingHandler:            ping.NewHandler(),
		containerPlatform:      config.ContainerPlatform,
	}
}

// resolveMetricsHandler returns the edge manager's shared metrics handler if available,
// otherwise creates a standalone one for the /api/metrics endpoint.
func resolveMetricsHandler(edgeManager *edge.Manager) *agentmetrics.Handler {
	if edgeManager != nil {
		if h := edgeManager.MetricsHandler(); h != nil {
			return h
		}
	}
	return agentmetrics.NewHandler()
}

// MetricsHandler returns the metrics handler for updating gauge values.
func (h *Handler) MetricsHandler() *agentmetrics.Handler {
	return h.metricsHandler
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/key") {
		h.keyHandler.ServeHTTP(rw, request)
		return
	}

	rw.Header().Set(agent.HTTPResponseAgentHeaderName, agent.Version)
	rw.Header().Set(agent.HTTPResponseAgentApiVersion, agent.APIVersion)

	// When the header is not set to PlatformDocker Portainer assumes the platform to be kubernetes.
	// However, Portainer should handle podman agents the same way as docker agents.
	agentPlatformIdentifier := h.containerPlatform
	if h.containerPlatform == agent.PlatformPodman {
		agentPlatformIdentifier = agent.PlatformDocker
	}
	rw.Header().Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(agentPlatformIdentifier)))

	switch {
	case strings.HasPrefix(request.URL.Path, "/v1/"):
		h.ServeHTTPV1(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v2/"):
		h.ServeHTTPV2(rw, request)
	case strings.HasPrefix(request.URL.Path, "/ping"):
		h.pingHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/api/metrics"):
		if !libhttp.IsLocalRequest(request) {
			http.Error(rw, "Forbidden", http.StatusForbidden)
			return
		}
		h.metricsHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/diagnostics"):
		h.diagnosticsHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/agents"):
		h.agentHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/host"):
		h.hostHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/browse"):
		h.browseHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/websocket"):
		h.webSocketHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/kubernetes"):
		h.kubernetesProxyHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}
