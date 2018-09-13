package host

import (
	"net/http"

	"github.com/gorilla/mux"
	httperror "github.com/portainer/agent/http/error"
)

// Handler represents an HTTP API Handler for host specific actions
type Handler struct {
	*mux.Router
}

// NewHandler returns a new instance of Handler
func NewHandler() *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/host/info",
		httperror.LoggerHandler(h.hostInfo)).Methods(http.MethodGet)

	return h
}
