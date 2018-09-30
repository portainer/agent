package handler

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/libhttp/error"
)

func agentProxyFactory(clusterService agent.ClusterService, agentTags map[string]string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return httperror.LoggerHandler(func(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

			if clusterService == nil {
				next.ServeHTTP(rw, r)
				return nil
			}

			agentTargetHeader := r.Header.Get(agent.HTTPTargetHeaderName)

			if agentTargetHeader == agentTags[agent.MemberTagKeyNodeName] || agentTargetHeader == "" {
				next.ServeHTTP(rw, r)
			} else {
				targetMember := clusterService.GetMemberByNodeName(agentTargetHeader)
				if targetMember == nil {
					return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent", agent.ErrAgentNotFound}
				}
				proxy.AgentHTTPRequest(rw, r, targetMember)
			}
			return nil
		})
	}
}
