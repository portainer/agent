package browse

import (
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

// GET request on /browse/ls?volumeID=:id&path=:path
func (handler *Handler) browseList(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
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

	files, err := filesystem.ListFilesInsideDirectory(path)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to list files inside specified directory", err}
	}

	return response.JSON(rw, files)
}

// GET request on /v1/browse/:id/get?path=:path
func (handler *Handler) browseListV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	path, err = filesystem.BuildPathToFileInsideVolume(volumeID, path)

	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	files, err := filesystem.ListFilesInsideDirectory(path)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to list files inside specified directory", err}
	}

	return response.JSON(rw, files)
}
