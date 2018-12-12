package ping

import (
	"net/http"

	"github.com/gorilla/mux"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API Handler executing a ping operation
type Handler struct {
	*mux.Router
}

// NewHandler returns a new instance of Handler
func NewHandler() *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/ping", httperror.LoggerHandler(h.ping)).Methods(http.MethodGet)
	return h
}
