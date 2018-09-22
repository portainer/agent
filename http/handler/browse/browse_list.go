package browse

import (
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) browseList(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

	path, err := request.RetrieveQueryParameter(r, "path", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	volumeID, _ := request.RetrieveQueryParameter(r, "volumeID", false)
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
