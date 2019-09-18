package proxy

import (
	"errors"
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
)

// AgentProxy enables redirection to different nodes
type AgentProxy struct {
	clusterService agent.ClusterService
	agentTags      map[string]string
	useTLS         bool
}

// NewAgentProxy returns a pointer to a new AgentProxy object
func NewAgentProxy(clusterService agent.ClusterService, agentTags map[string]string, useTLS bool) *AgentProxy {
	return &AgentProxy{
		clusterService: clusterService,
		agentTags:      agentTags,
		useTLS:         useTLS,
	}
}

// Redirect is redirecting request to the specific agent node
func (p *AgentProxy) Redirect(next http.Handler) http.Handler {
	return httperror.LoggerHandler(func(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

		if p.clusterService == nil {
			next.ServeHTTP(rw, r)
			return nil
		}

		agentTargetHeader := r.Header.Get(agent.HTTPTargetHeaderName)

		if agentTargetHeader == p.agentTags[agent.MemberTagKeyNodeName] || agentTargetHeader == "" {
			next.ServeHTTP(rw, r)
		} else {
			targetMember := p.clusterService.GetMemberByNodeName(agentTargetHeader)
			if targetMember == nil {
				return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent", errors.New("Unable to find the targeted agent")}
			}
			AgentHTTPRequest(rw, r, targetMember, p.useTLS)
		}
		return nil
	})
}
