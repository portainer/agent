package kubernetes

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/http/security"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
)

// Handler is the HTTP handler used to handle volume browsing operations.
type Handler struct {
	*mux.Router
	kubernetesDeployer *exec.KubernetesDeployer
}

// NewHandler returns a pointer to an Handler
func NewHandler(notaryService *security.NotaryService, kubernetesDeployer *exec.KubernetesDeployer) *Handler {
	h := &Handler{
		Router:             mux.NewRouter(),
		kubernetesDeployer: kubernetesDeployer,
	}

	h.Handle("/kubernetes/stack",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.kubernetesDeploy))).Methods(http.MethodPost)

	h.Handle("/kubernetes/namespaces",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.kubernetesGetNamespaces))).Methods(http.MethodGet)
	h.Handle("/kubernetes/namespaces/{namespace}",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.kubernetesGetNamespaces))).Methods(http.MethodGet)

	h.Handle("/kubernetes/namespaces/{namespace}/{configmaps|secrets}",
		notaryService.DigitalSignatureVerification(httperror.LoggerHandler(h.kubernetesGetConfigMaps))).Methods(http.MethodGet)

	return h
}
