package host

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/proxy"
)

// Handler represents an HTTP API Handler for host specific actions
type Handler struct {
	*mux.Router
	clusterService agent.ClusterService
	agentTags      map[string]string
}

// NewHandler returns a new instance of Handler
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		clusterService: clusterService,
		agentTags:      agentTags,
	}

	h.Handle("/host/info",
		h.agentProxy(httperror.LoggerHandler(h.hostInfo))).Methods(http.MethodGet)

	return h
}

func (handler *Handler) agentProxy(next http.Handler) http.Handler {
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
