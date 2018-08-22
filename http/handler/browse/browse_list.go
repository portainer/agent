package browse

import (
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/request"
	"github.com/portainer/agent/http/response"
)

func (handler *Handler) browseList(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	files, err := filesystem.ListFilesInsideVolumeDirectory(volumeID, path)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to list files inside specified directory", err}
	}

	return response.JSON(rw, files)
}
