package browse

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/proxy"
)

// Handler is the HTTP handler used to handle volume browsing operations.
type Handler struct {
	*mux.Router
	clusterService agent.ClusterService
	agentTags      map[string]string
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Browse related HTTP endpoints.
func NewHandler(clusterService agent.ClusterService, agentTags map[string]string) *Handler {
	h := &Handler{
		Router:         mux.NewRouter(),
		clusterService: clusterService,
		agentTags:      agentTags,
	}

	h.Handle("/browse/{id}/ls",
		h.agentProxy(httperror.LoggerHandler(h.browseList))).Methods(http.MethodGet)
	h.Handle("/browse/{id}/get",
		h.agentProxy(httperror.LoggerHandler(h.browseGet))).Methods(http.MethodGet)
	h.Handle("/browse/{id}/delete",
		h.agentProxy(httperror.LoggerHandler(h.browseDelete))).Methods(http.MethodDelete)
	h.Handle("/browse/{id}/rename",
		h.agentProxy(httperror.LoggerHandler(h.browseRename))).Methods(http.MethodPut)
	h.Handle("/browse/{id}/put",
		h.agentProxy(httperror.LoggerHandler(h.browsePut))).Methods(http.MethodPut)
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
