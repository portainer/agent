package browse

import (
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

// DELETE request on /browse/delete?volumeID=:id&path=:path
func (handler *Handler) browseDelete(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, _ := request.RetrieveQueryParameter(r, "volumeID", true)
	path, err := request.RetrieveQueryParameter(r, "path", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	if volumeID != "" {
		path, err = filesystem.BuildPathToFileInsideVolume(volumeID, path)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume", err}
		}
	}

	err = filesystem.RemoveFile(path)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to remove file", err}
	}

	return response.Empty(rw)
}

// DELETE request on /v1/browse/:id/delete
func (handler *Handler) browseDeleteV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	fullPath, err := filesystem.BuildPathToFileInsideVolume(volumeID, path)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	err = filesystem.RemoveFile(fullPath)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to remove file", err}
	}

	return response.Empty(rw)
}
