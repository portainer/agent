package browse

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/request"
	"github.com/portainer/agent/http/response"
)

type browseUploadPayload struct {
	FilePath string
	FileName string
	File     []byte
}

func (payload *browseUploadPayload) Validate(r *http.Request) error {
	path, err := request.RetrieveMultiPartFormValue(r, "Path", false)
	if err != nil {
		return agent.Error("Invalid file path")
	}
	payload.FilePath = path

	filename, err := request.RetrieveMultiPartFormValue(r, "Filename", false)
	if err != nil {
		return agent.Error("Invalid file name")
	}
	payload.FileName = filename

	file, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return agent.Error("Invalid upload file")
	}
	payload.File = file

	return nil
}

func (handler *Handler) browsePut(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}
	var payload browseUploadPayload
	err = request.DecodeAndValidateJSONPayload(r, &payload)

	err = filesystem.UploadFileToVolume(volumeID, payload.FilePath, payload.FileName, payload.File)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}
