package nomadproxy

import (
	"net/http"

	"github.com/portainer/agent"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler represents an HTTP API handler for proxying requests to the Nomad API.
type Handler struct {
	*mux.Router
	nomadProxy  http.Handler
	nomadConfig agent.NomadConfig
}

// NewHandler returns a new instance of Handler.
// It sets the associated handle functions for all the Nomad related HTTP endpoints.
func NewHandler(notaryService *security.NotaryService, nomadConfig agent.NomadConfig) *Handler {
	h := &Handler{
		Router:      mux.NewRouter(),
		nomadProxy:  proxy.NewNomadProxy(nomadConfig),
		nomadConfig: nomadConfig,
	}

	h.PathPrefix("/").Handler(notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.nomadOperation)))
	return h
}
