package browse

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type browsePutPayload struct {
	Path     string
	Filename string
	File     []byte
}

func (payload *browsePutPayload) Validate(r *http.Request) error {
	path, err := request.RetrieveMultiPartFormValue(r, "Path", false)
	if err != nil {
		return agent.Error("Invalid file path")
	}
	payload.Path = path

	file, filename, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return agent.Error("Invalid uploaded file")
	}
	payload.File = file
	payload.Filename = filename

	return nil
}

func (handler *Handler) browsePut(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

	var payload browsePutPayload
	err := payload.Validate(r)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	volumeID, err := request.RetrieveQueryParameter(r, "volumeID", true)
	if volumeID != "" {
		payload.Path, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Path)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume", err}
		}
	}

	err = filesystem.UploadFile(payload.Path, payload.Filename, payload.File)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}

func (handler *Handler) browsePutV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	var payload browsePutPayload
	err = payload.Validate(r)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}
	payload.Path, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Path)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume", err}
	}

	err = filesystem.UploadFile(payload.Path, payload.Filename, payload.File)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}
