package handler

import (
	"net/http"

	"github.com/portainer/libhttp/response"
)

func (h *Handler) Ping(rw http.ResponseWriter, request *http.Request) {
	response.Empty(rw)
}
