package ping

import (
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

func (h *Handler) ping(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	return response.Empty(rw)
}
