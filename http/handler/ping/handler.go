package ping

import (
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"

	"github.com/gorilla/mux"
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
