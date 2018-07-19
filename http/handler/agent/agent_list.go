package agent

import (
	"net/http"

	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/response"
)

func (handler *Handler) agentList(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	members := handler.clusterService.Members()
	return response.JSON(w, members)
}
