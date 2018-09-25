package agentproxy

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle proxying to a specific node.
type Handler struct {
	*mux.Router
	clusterService agent.ClusterService
	agentTags      map[string]string
}

func (handler *Handler) CreateAgentProxyHandler(cs agent.ClusterService, agentTags map[string]string) {
	handler.clusterService = cs
	handler.agentTags = agentTags
	handler.Router = mux.NewRouter()
}

func (handler *Handler) AgentProxy(next http.Handler) http.Handler {
	return httperror.LoggerHandler(func(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

		if handler.clusterService == nil {
			next.ServeHTTP(rw, r)
			return nil
		}

		agentTargetHeader := r.Header.Get(agent.HTTPTargetHeaderName)

		if agentTargetHeader == handler.agentTags[agent.MemberTagKeyNodeName] || agentTargetHeader == "" {
			next.ServeHTTP(rw, r)
		} else {
			targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
			if targetMember == nil {
				return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent", agent.ErrAgentNotFound}
			}
			proxy.AgentHTTPRequest(rw, r, targetMember)
		}
		return nil
	})
}
