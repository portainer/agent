package key

import (
	"errors"
	"net/http"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

type keyInspectResponse struct {
	Key string `json:"key"`
}

func (handler *Handler) keyInspect(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	if !handler.edgeMode {
		return &httperror.HandlerError{http.StatusServiceUnavailable, "Edge key management is disabled on non Edge agent", errors.New("Edge key management is disabled")}
	}

	if !handler.edgeKeyService.IsKeySet() {
		return &httperror.HandlerError{http.StatusNotFound, "No key associated to this agent", errors.New("Edge key unavailable")}
	}

	edgeKey := handler.edgeKeyService.GetKey()

	return response.JSON(w, keyInspectResponse{
		Key: edgeKey,
	})
}
