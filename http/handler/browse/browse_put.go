package browse

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/request"
	"github.com/portainer/agent/http/response"
)

type browsePutPayload struct {
	FilePath string
}

func (payload *browsePutPayload) Validate(r *http.Request) error {
	path, err := request.RetrieveMultiPartFormValue(r, "Path", false)
	if err != nil {
		return agent.Error("Invalid file path")
	}
	payload.FilePath = path

	return nil
}

func (handler *Handler) browsePut(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	var payload browsePutPayload
	err = payload.Validate(r)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	file, filename, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid Uploaded File", err}
	}

	err = filesystem.UploadFileToVolume(volumeID, payload.FilePath, filename, file)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}
