package dockerhub

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
)

// Handler represents an HTTP API Handler for host specific actions
type Handler struct {
	*mux.Router
	PullLimitCheckDisabled bool
}

// NewHandler returns a new instance of Handler
func NewHandler(notaryService *security.NotaryService, pullLimitCheckDisabled bool) *Handler {
	h := &Handler{
		Router:                 mux.NewRouter(),
		PullLimitCheckDisabled: pullLimitCheckDisabled,
	}

	h.Handle("/dockerhub",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.dockerhubStatus))).Methods(http.MethodPost)

	return h
}
