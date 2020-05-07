package key

import (
	"errors"
	"log"
	"net/http"

	"github.com/portainer/libhttp/request"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
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
	if !handler.edgeManager.IsEdgeModeEnabled() {
		return &httperror.HandlerError{http.StatusServiceUnavailable, "Edge key management is disabled on non Edge agent", errors.New("Edge key management is disabled")}
	}

	if handler.edgeManager.IsKeySet() {
		return &httperror.HandlerError{http.StatusConflict, "An Edge key is already associated to this agent", errors.New("Edge key already associated")}
	}

	log.Println("[INFO] [http,api] [message: Received Edge key association request]")

	var payload keyCreatePayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	err = handler.edgeManager.SetKey(payload.Key)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to associate Edge key", err}
	}

	err = handler.edgeManager.Start()
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to start Edge Manager", err}
	}

	return response.Empty(w)
}
