package websocket

import (
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/security"
	"github.com/portainer/agent/kubernetes"
	httperror "github.com/portainer/libhttp/error"
)

type (
	// Handler represents an HTTP API handler for proxying requests to a web socket.
	Handler struct {
		*mux.Router
		clusterService       agent.ClusterService
		connectionUpgrader   websocket.Upgrader
		runtimeConfiguration *agent.RuntimeConfiguration
		kubeClient           *kubernetes.KubeClient
	}

	execStartOperationPayload struct {
		Tty    bool
		Detach bool
	}
)

// NewHandler returns a new instance of Handler.
func NewHandler(clusterService agent.ClusterService, config *agent.RuntimeConfiguration, notaryService *security.NotaryService, kubeClient *kubernetes.KubeClient) *Handler {
	h := &Handler{
		Router:               mux.NewRouter(),
		connectionUpgrader:   websocket.Upgrader{},
		clusterService:       clusterService,
		runtimeConfiguration: config,
		kubeClient:           kubeClient,
	}

	h.Handle("/websocket/attach", notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.websocketAttach)))
	h.Handle("/websocket/exec", notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.websocketExec)))
	h.Handle("/websocket/pod", notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.websocketPodExec)))
	return h
}
