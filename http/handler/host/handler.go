package host

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API Handler for host specific actions
type Handler struct {
	*mux.Router
	systemService agent.SystemService
}

// NewHandler returns a new instance of Handler
func NewHandler(systemService agent.SystemService, agentProxy *proxy.AgentProxy, notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router:        mux.NewRouter(),
		systemService: systemService,
	}

	h.Handle("/host/info",
		agentProxy.Redirect(notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.hostInfo)))).Methods(http.MethodGet)

	return h
}
