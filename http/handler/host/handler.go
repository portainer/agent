package host

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler/agentproxy"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API Handler for host specific actions
type Handler struct {
	agentproxy.Handler
	systemService agent.SystemService
}

// NewHandler returns a new instance of Handler
func NewHandler(systemService agent.SystemService, clusterService agent.ClusterService, agentTags map[string]string) *Handler {
	h := &Handler{
		systemService: systemService,
	}
	h.CreateAgentProxyHandler(clusterService, agentTags)

	h.Handle("/host/info",
		h.AgentProxy(httperror.LoggerHandler(h.hostInfo))).Methods(http.MethodGet)

	return h
}
