package agent

import (
	"net/http"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) agentList(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	if handler.clusterService == nil {
		return &httperror.HandlerError{http.StatusServiceUnavailable, "Agent management is not available when running the agent on a standalone engine", errAgentManagementDisabled}
	}

	members := handler.clusterService.Members()
	return response.JSON(w, members)
}
