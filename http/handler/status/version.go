package status

import (
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) version(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	agentInfo := struct {
		Version string
	}{
		Version: agent.Version,
	}

	return response.JSON(w, agentInfo)
}
