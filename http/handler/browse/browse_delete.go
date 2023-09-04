package browse

import (
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

// DELETE request on /browse/delete?volumeID=:id&path=:path
func (handler *Handler) browseDelete(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
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

	err = filesystem.RemoveFile(path)
	if err != nil {
		return httperror.InternalServerError("Unable to remove file", err)
	}

	return response.Empty(rw)
}

// DELETE request on /v1/browse/:id/delete
func (handler *Handler) browseDeleteV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid volume identifier route variable", err)
	}

	path, err := request.RetrieveQueryParameter(r, "path", false)
	fullPath, err := filesystem.BuildPathToFileInsideVolume(volumeID, path)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: path", err)
	}

	err = filesystem.RemoveFile(fullPath)
	if err != nil {
		return httperror.InternalServerError("Unable to remove file", err)
	}

	return response.Empty(rw)
}
