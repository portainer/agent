package browse

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle volume browsing operations.
type Handler struct {
	*mux.Router
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Browse related HTTP endpoints.
func NewHandler(agentProxy *proxy.AgentProxy, notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/browse/ls",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseList)))).Methods(http.MethodGet)
	h.Handle("/browse/get",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseGet)))).Methods(http.MethodGet)
	h.Handle("/browse/delete",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseDelete)))).Methods(http.MethodDelete)
	h.Handle("/browse/rename",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseRename)))).Methods(http.MethodPut)
	h.Handle("/browse/put",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browsePut)))).Methods(http.MethodPost)
	return h
}

// NewHandlerV1 returns a pointer to an Handler
// It sets the associated handle functions for all the Browse related HTTP endpoints.
func NewHandlerV1(agentProxy *proxy.AgentProxy, notaryService *security.NotaryService) *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/browse/{id}/ls",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseListV1)))).Methods(http.MethodGet)
	h.Handle("/browse/{id}/get",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseGetV1)))).Methods(http.MethodGet)
	h.Handle("/browse/{id}/delete",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseDeleteV1)))).Methods(http.MethodDelete)
	h.Handle("/browse/{id}/rename",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browseRenameV1)))).Methods(http.MethodPut)
	h.Handle("/browse/{id}/put",
		notaryService.DigitalSignatureVerification(agentProxy.Redirect(httperror.LoggerHandler(h.browsePutV1)))).Methods(http.MethodPost)
	return h
}
