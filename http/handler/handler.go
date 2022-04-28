package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge"
	"github.com/portainer/agent/exec"
	httpagenthandler "github.com/portainer/agent/http/handler/agent"
	"github.com/portainer/agent/http/handler/browse"
	"github.com/portainer/agent/http/handler/docker"
	"github.com/portainer/agent/http/handler/dockerhub"
	"github.com/portainer/agent/http/handler/host"
	"github.com/portainer/agent/http/handler/key"
	"github.com/portainer/agent/http/handler/kubernetes"
	"github.com/portainer/agent/http/handler/kubernetesproxy"
	"github.com/portainer/agent/http/handler/nomadproxy"
	"github.com/portainer/agent/http/handler/ping"
	"github.com/portainer/agent/http/handler/websocket"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	kubecli "github.com/portainer/agent/kubernetes"
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
	nomadProxyHandler      *nomadproxy.Handler
	webSocketHandler       *websocket.Handler
	hostHandler            *host.Handler
	pingHandler            *ping.Handler
	containerPlatform      agent.ContainerPlatform
}

// Config represents a server handler configuration
// used to create a new handler
type Config struct {
	SystemService        agent.SystemService
	ClusterService       agent.ClusterService
	SignatureService     agent.DigitalSignatureService
	KubeClient           *kubecli.KubeClient
	KubernetesDeployer   *exec.KubernetesDeployer
	EdgeManager          *edge.Manager
	RuntimeConfiguration *agent.RuntimeConfiguration
	NomadConfig          agent.NomadConfig
	Secured              bool
	ContainerPlatform    agent.ContainerPlatform
}

var dockerAPIVersionRegexp = regexp.MustCompile(`(/v[0-9]\.[0-9]*)?`)

// NewHandler returns a pointer to a Handler.
func NewHandler(config *Config) *Handler {
	agentProxy := proxy.NewAgentProxy(config.ClusterService, config.RuntimeConfiguration, config.Secured)
	notaryService := security.NewNotaryService(config.SignatureService, true)

	return &Handler{
		agentHandler:           httpagenthandler.NewHandler(config.ClusterService, notaryService),
		browseHandler:          browse.NewHandler(agentProxy, notaryService),
		browseHandlerV1:        browse.NewHandlerV1(agentProxy, notaryService),
		dockerProxyHandler:     docker.NewHandler(config.ClusterService, config.RuntimeConfiguration, notaryService, config.Secured),
		dockerhubHandler:       dockerhub.NewHandler(notaryService),
		keyHandler:             key.NewHandler(notaryService, config.EdgeManager),
		kubernetesHandler:      kubernetes.NewHandler(notaryService, config.KubernetesDeployer),
		kubernetesProxyHandler: kubernetesproxy.NewHandler(notaryService),
		nomadProxyHandler:      nomadproxy.NewHandler(notaryService, config.NomadConfig),
		webSocketHandler:       websocket.NewHandler(config.ClusterService, config.RuntimeConfiguration, notaryService, config.KubeClient),
		hostHandler:            host.NewHandler(config.SystemService, agentProxy, notaryService),
		pingHandler:            ping.NewHandler(),
		containerPlatform:      config.ContainerPlatform,
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/key") {
		h.keyHandler.ServeHTTP(rw, request)
		return
	}

	request.URL.Path = dockerAPIVersionRegexp.ReplaceAllString(request.URL.Path, "")
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
	case strings.HasPrefix(request.URL.Path, "/kubernetes"):
		h.kubernetesProxyHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/nomad"):
		h.nomadProxyHandler.ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}
