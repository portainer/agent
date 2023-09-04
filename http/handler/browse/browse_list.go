package browse

import (
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

// GET request on /browse/ls?volumeID=:id&path=:path
func (handler *Handler) browseList(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, _ := request.RetrieveQueryParameter(r, "volumeID", true)
	path, err := request.RetrieveQueryParameter(r, "path", false)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: path", err)
	}

	if volumeID != "" {
		path, err = filesystem.BuildPathToFileInsideVolume(volumeID, path)
		if err != nil {
			return httperror.BadRequest("Invalid volume", err)
		}
	}

	files, err := filesystem.ListFilesInsideDirectory(path)
	if err != nil {
		return httperror.InternalServerError("Unable to list files inside specified directory", err)
	}

	return response.JSON(rw, files)
}

// GET request on /v1/browse/:id/get?path=:path
func (handler *Handler) browseListV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid volume identifier route variable", err)
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	path, err = filesystem.BuildPathToFileInsideVolume(volumeID, path)

	if err != nil {
		return httperror.BadRequest("Invalid query parameter: path", err)
	}

	files, err := filesystem.ListFilesInsideDirectory(path)
	if err != nil {
		return httperror.InternalServerError("Unable to list files inside specified directory", err)
	}

	return response.JSON(rw, files)
}
