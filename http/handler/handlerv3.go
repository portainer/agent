package handler

import (
	"net/http"
	"strings"
)

// ServeHTTPV3 is the HTTP router for all v2 api requests.
func (h *Handler) ServeHTTPV3(rw http.ResponseWriter, request *http.Request) {
	switch {
	case strings.HasPrefix(request.URL.Path, "/v3/ping"):
		http.StripPrefix("/v3", h.pingHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v3/agents"):
		http.StripPrefix("/v3", h.agentHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v3/host"):
		http.StripPrefix("/v3", h.hostHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v3/browse"):
		http.StripPrefix("/v3", h.browseHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/v3/websocket"):
		http.StripPrefix("/v3", h.webSocketHandler).ServeHTTP(rw, request)
	case strings.HasPrefix(request.URL.Path, "/"):
		h.dockerProxyHandler.ServeHTTP(rw, request)
	}
}
