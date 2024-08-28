package browse

import (
	"errors"
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type browseRenamePayload struct {
	CurrentFilePath string
	NewFilePath     string
}

func (payload *browseRenamePayload) Validate(r *http.Request) error {
	if len(payload.CurrentFilePath) != 0 {
		return errors.New("Current file path is invalid")
	}
	if len(payload.NewFilePath) != 0 {
		return errors.New("New file path is invalid")
	}
	return nil
}

// PUT request on /browse/rename?volumeID=:id
func (handler *Handler) browseRename(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, _ := request.RetrieveQueryParameter(r, "volumeID", true)
	var payload browseRenamePayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}

	if volumeID != "" {
		payload.CurrentFilePath, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.CurrentFilePath)
		if err != nil {
			return httperror.BadRequest("Invalid volume", err)
		}
		payload.NewFilePath, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.NewFilePath)
		if err != nil {
			return httperror.BadRequest("Invalid volume", err)
		}
	}

	err = filesystem.RenameFile(payload.CurrentFilePath, payload.NewFilePath)
	if err != nil {
		return httperror.InternalServerError("Unable to rename file", err)
	}

	return response.Empty(rw)
}

// PUT request on /v1/browse/:id/rename
func (handler *Handler) browseRenameV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid volume identifier route variable", err)
	}

	var payload browseRenamePayload
	err = request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}

	payload.CurrentFilePath, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.CurrentFilePath)
	if err != nil {
		return httperror.BadRequest("Invalid volume", err)
	}
	payload.NewFilePath, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.NewFilePath)
	if err != nil {
		return httperror.BadRequest("Invalid volume", err)
	}

	err = filesystem.RenameFile(payload.CurrentFilePath, payload.NewFilePath)
	if err != nil {
		return httperror.InternalServerError("Unable to rename file", err)
	}

	return response.Empty(rw)
}
