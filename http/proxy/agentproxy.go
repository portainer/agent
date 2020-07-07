package proxy

import (
	"errors"
	"log"
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
)

// AgentProxy enables redirection to different nodes
type AgentProxy struct {
	clusterService       agent.ClusterService
	runtimeConfiguration *agent.RuntimeConfiguration
	useTLS               bool
}

// NewAgentProxy returns a pointer to a new AgentProxy object
func NewAgentProxy(clusterService agent.ClusterService, config *agent.RuntimeConfiguration, useTLS bool) *AgentProxy {
	return &AgentProxy{
		clusterService:       clusterService,
		runtimeConfiguration: config,
		useTLS:               useTLS,
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

		if agentTargetHeader == p.runtimeConfiguration.NodeName || agentTargetHeader == "" {
			next.ServeHTTP(rw, r)
		} else {
			targetMember := p.clusterService.GetMemberByNodeName(agentTargetHeader)
			if targetMember == nil {
				log.Printf("[ERROR] [http,agent,proxy] [target_node: %s] [request: %s] [message: unable to redirect request to specified node: agent not found in cluster]", agentTargetHeader, r.URL)
				return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent", errors.New("Unable to find the targeted agent")}
			}
			AgentHTTPRequest(rw, r, targetMember, p.useTLS)
		}
		return nil
	})
}
