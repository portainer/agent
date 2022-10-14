package agent

import (
	"log"
	"net/http"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) agentUpgrade(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	targetVersion, _ := request.RetrieveQueryParameter(r, "target_version", true)
	log.Println("target version = ", targetVersion)

	// todo: start container-upgrader container with portainer_agent docker container id

	return response.JSON(w, targetVersion)
}
