package sosivio

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle Sosivio operations.
type Handler struct {
	*mux.Router
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Sosivio related HTTP endpoints.
func NewHandler(notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/sosivio/namespaces",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.namespaces))).Methods(http.MethodGet)

	return h
}
