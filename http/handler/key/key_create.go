package key

import (
	"errors"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"

	"github.com/rs/zerolog/log"
)

type keyCreatePayload struct {
	Key string
}

func (payload *keyCreatePayload) Validate(r *http.Request) error {
	if payload.Key == "" {
		return errors.New("invalid key")
	}
	return nil
}

func (handler *Handler) keyCreate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	if handler.edgeManager == nil {
		return &httperror.HandlerError{StatusCode: http.StatusServiceUnavailable, Message: "Edge key management is disabled on non Edge agent", Err: errors.New("Edge key management is disabled")}
	}

	if handler.edgeManager.IsKeySet() {
		return &httperror.HandlerError{StatusCode: http.StatusConflict, Message: "An Edge key is already associated to this agent", Err: errors.New("Edge key already associated")}
	}

	log.Info().Msg("received Edge key association request")

	var payload keyCreatePayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}

	err = handler.edgeManager.SetKey(payload.Key)
	if err != nil {
		return httperror.InternalServerError("Unable to associate Edge key", err)
	}

	err = handler.edgeManager.Start()
	if err != nil {
		return httperror.InternalServerError("Unable to start Edge Manager", err)
	}

	return response.Empty(w)
}
