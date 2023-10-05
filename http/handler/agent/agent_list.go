package agent

import (
	"errors"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

func (handler *Handler) agentList(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	if handler.clusterService == nil {
		return httperror.NewError(http.StatusServiceUnavailable, "Agent management is not available when running the agent on a standalone engine", errors.New("Agent management is disabled"))
	}

	members := handler.clusterService.Members()
	return response.JSON(w, members)
}
