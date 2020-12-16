package dockerhub

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API Handler for host specific actions
type Handler struct {
	*mux.Router
}

// NewHandler returns a new instance of Handler
func NewHandler(notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/dockerhub",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.dockerhubStatus))).Methods(http.MethodPost)

	return h
}
