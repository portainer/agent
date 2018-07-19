package browse

import (
	"net/http"

	"bitbucket.org/portainer/agent/filesystem"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/request"
)

func (handler *Handler) browseGet(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: path", err}
	}

	fileDetails, err := filesystem.OpenFileInsideVolume(volumeID, path)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to open file", err}
	}
	defer fileDetails.File.Close()

	http.ServeContent(rw, r, fileDetails.BasePath, fileDetails.ModTime, fileDetails.File)
	return nil
}
