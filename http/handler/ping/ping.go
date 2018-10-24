package ping

import (
	"net/http"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

func (h *Handler) ping(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	return response.Empty(rw)
}
