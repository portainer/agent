package status

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

type Handler struct {
	*mux.Router
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the agent related HTTP endpoints.
func NewHandler(notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/version",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.version))).Methods(http.MethodGet)

	return h
}
