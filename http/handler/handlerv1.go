package handler

import (
	"net/http"
	"strings"
)

// ServeHTTPV1 is the HTTP router for all v1 api requests.
func (h *Handler) ServeHTTPV1(rw http.ResponseWriter, request *http.Request) {
	switch {
	case strings.HasPrefix(request.URL.Path, "/v1/agents"):
		http.StripPrefix("/v1", h.agentHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v1/host"):
		http.StripPrefix("/v1", h.hostHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v1/browse"):
		http.StripPrefix("/v1", h.browseHandlerV1).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v1/websocket"):
		http.StripPrefix("/v1", h.webSocketHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}
