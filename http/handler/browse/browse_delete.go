package browse

import (
	"net/http"

	"bitbucket.org/portainer/agent/filesystem"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/request"
	"bitbucket.org/portainer/agent/http/response"
)

func (handler *Handler) browseDelete(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	err = filesystem.RemoveFileInsideVolume(volumeID, path)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to remove file", err}
	}

	return response.Empty(rw)
}
