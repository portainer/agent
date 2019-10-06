package kubernetes

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API handler for proxying requests to the Kubernetes API.
type Handler struct {
	*mux.Router
	kubernetesProxy http.Handler
}

// NewHandler returns a new instance of Handler.
// It sets the associated handle functions for all the Kubernetes related HTTP endpoints.
func NewHandler(notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router:          mux.NewRouter(),
		kubernetesProxy: proxy.NewKubernetesProxy(),
	}

	h.PathPrefix("/").Handler(notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.kubernetesOperation)))
	return h
}
