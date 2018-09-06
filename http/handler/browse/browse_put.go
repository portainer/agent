package browse

import (
	"net/http"

	"github.com/asaskevich/govalidator"
	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/request"
	"github.com/portainer/agent/http/response"
)

type browseUploadPayload struct {
	NewFilePath string
}

func (payload *browseUploadPayload) Validate(r *http.Request) error {
	if govalidator.IsNull(payload.NewFilePath) {
		return agent.Error("New file path is invalid")
	}
	return nil
}

func (handler *Handler) browsePut(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	var payload browseUploadPayload
	err = request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	r.ParseMultipartForm(agent.UploadMaxMemory)
	file, fhandler, err := r.FormFile("uploadfile")
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Invalid uploadfile", err}
	}

	err = filesystem.UploadFileToVolume(file, fhandler, volumeID, payload.NewFilePath)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}
