package browse

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/libhttp/error"
)

// Handler is the HTTP handler used to handle volume browsing operations.
type Handler struct {
	*mux.Router
}

// NewHandler returns a pointer to an Handler
// It sets the associated handle functions for all the Browse related HTTP endpoints.
func NewHandler(agentProxy *proxy.AgentProxy) *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.Handle("/browse/ls",
		agentProxy.Redirect(httperror.LoggerHandler(h.browseList))).Methods(http.MethodGet)
	h.Handle("/browse/get",
		agentProxy.Redirect(httperror.LoggerHandler(h.browseGet))).Methods(http.MethodGet)
	h.Handle("/browse/delete",
		agentProxy.Redirect(httperror.LoggerHandler(h.browseDelete))).Methods(http.MethodDelete)
	h.Handle("/browse/rename",
		agentProxy.Redirect(httperror.LoggerHandler(h.browseRename))).Methods(http.MethodPut)
	h.Handle("/browse/put",
		agentProxy.Redirect(httperror.LoggerHandler(h.browsePut))).Methods(http.MethodPost)
	return h
}
