package kubernetes

import (
	"context"
	"net/http"
	"path"
	"strings"

	"github.com/portainer/agent"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func (handler *Handler) kubernetesGetNamespaces(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	log.Debug().Msgf("GetNamespaces Handler: Request: %s %s", r.Method, r.URL.Path)

	config, err := rest.InClusterConfig()
	if err != nil {
		return httperror.InternalServerError("Unable to read service account token file", err)
	}

	token := r.Header.Get(agent.HTTPKubernetesSATokenHeaderName)
	if len(token) == 0 {
		config.BearerToken = token
	}

	// adjust the API path to match the Kubernetes API
	api := path.Join("/api/v1/", strings.TrimPrefix(r.URL.Path, "/kubernetes"))

	// Create an HTTP client from the Kubernetes configuration
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return httperror.InternalServerError("Unable to create HTTP client", err)
	}

	restClient := clientSet.RESTClient()

	// Create an HTTP request using the client
	req := restClient.Get().RequestURI(api)

	// Send the HTTP request
	resp, err := req.DoRaw(context.Background())
	if err != nil {
		panic(err)
	}

	return response.JSON(rw, string(resp))
}
