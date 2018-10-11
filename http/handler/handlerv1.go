package handler

import (
	"net/http"
	"strings"
)

func (h *Handler) ServeHTTPV1(rw http.ResponseWriter, request *http.Request) {
	switch {
	case strings.HasPrefix(request.URL.Path, "/v1/browse"):
		http.StripPrefix("/v1", h.browseHandlerV1).ServeHTTP(rw, request)
	}
}
