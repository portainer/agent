package key

import (
	"errors"
	"log"
	"net/http"

	"github.com/portainer/agent"

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
	if !handler.edgeMode {
		return &httperror.HandlerError{http.StatusServiceUnavailable, "Edge key management is disabled on non Edge agent", errors.New("Edge key management is disabled")}
	}

	if handler.tunnelOperator.IsKeySet() {
		return &httperror.HandlerError{http.StatusForbidden, "An Edge key is already associated to this agent", errors.New("Edge key already associated")}
	}

	log.Println("[INFO] [http,api] [message: Received Edge key association request]")

	var payload keyCreatePayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	err = handler.tunnelOperator.SetKey(payload.Key)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to associate Edge key", err}
	}

	if handler.clusterService != nil {
		tags := handler.clusterService.GetTags()
		tags[agent.MemberTagEdgeKeySet] = "set"
		err = handler.clusterService.UpdateTags(tags)
		if err != nil {
			return &httperror.HandlerError{http.StatusInternalServerError, "Unable to update agent cluster tags", err}
		}
	}

	go handler.tunnelOperator.Start()

	return response.Empty(w)
}
