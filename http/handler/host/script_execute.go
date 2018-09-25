package host

import (
	"net/http"

	"github.com/asaskevich/govalidator"
	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type scriptPayload struct {
	Script string
}

func (payload *scriptPayload) Validate(r *http.Request) error {
	if govalidator.IsNull(payload.Script) {
		return agent.Error("Script is invalid")
	}

	return nil
}

func (handler *Handler) executeScript(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload scriptPayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	response.JSON(rw, "execute script")
	return nil
}
