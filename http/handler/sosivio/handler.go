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
	h.Handle("/sosivio/commandcenter",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.commandCenter))).Methods(http.MethodGet)
	return h
}

// TODO: REVIEW-POC-SOSIVIO
// Port to libhttp.
// Raw returns raw data. Returns a pointer to a
// HandlerError if encoding fails.
func Raw(rw http.ResponseWriter, data []byte) *httperror.HandlerError {
	rw.Write(data)
	return nil
}
