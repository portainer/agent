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
	Path string
	File []byte
}

func (payload *browsePutPayload) Validate(r *http.Request) error {
	file, path, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return agent.Error("Invalid file")
	}
	payload.Path = path
	payload.File = file
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

	err = filesystem.UploadFileInVolume(volumeID, payload.Path, payload.File)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}
