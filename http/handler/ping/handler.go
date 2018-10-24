package ping

import (
	"net/http"

	"github.com/gorilla/mux"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle ping.
type Handler struct {
	*mux.Router
}

// NewHandler returns a pointer to an Handler
func NewHandler() *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/ping", httperror.LoggerHandler(h.ping)).Methods(http.MethodGet)

	return h
}
