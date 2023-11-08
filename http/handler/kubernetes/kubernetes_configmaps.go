package kubernetes

import (
	"context"
	"net/http"
	"path"
	"strings"

	"github.com/portainer/agent"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// type (
// 	FilteredNamespaceResponse struct {
// 		Kind     string `json:"kind"`
// 		Name     string `json:"name"`
// 		MetaData struct {
// 			Name              string `json:"name"`
// 			CreationTimestamp string `json:"creationTimestamp"`
// 		} `json:"metadata"`
// 	}

// 	FilteredNamespacesResponse struct {
// 		APIVersion string `json:"apiVersion"`
// 		Items      []struct {
// 			Metadata struct {
// 				CreationTimestamp string `json:"creationTimestamp"`
// 				Labels            struct {
// 					Kubernetes_io_metadata_name string `json:"kubernetes.io/metadata.name"`
// 				} `json:"labels"`
// 				Name            string `json:"name"`
// 				ResourceVersion string `json:"resourceVersion"`
// 				UID             string `json:"uid"`
// 			} `json:"metadata"`
// 		} `json:"items"`
// 		Kind     string `json:"kind"`
// 		Metadata struct {
// 			ResourceVersion string `json:"resourceVersion"`
// 		} `json:"metadata"`
// 	}
// )

func (handler *Handler) kubernetesGetConfigMaps(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
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
	if len(r.URL.RawQuery) > 0 {
		api = api + "?" + r.URL.RawQuery
	}

	log.Debug().Msgf("New API path: %s", api)

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

	return filteredConfigMaps(rw, resp)
}

func filteredConfigMaps(rw http.ResponseWriter, data []byte) *httperror.HandlerError {
	// var namespacesResponse []FilteredNamespaceResponse
	// err := json.Unmarshal([]byte(data), &namespacesResponse)
	// if err != nil {
	// 	return httperror.InternalServerError("Unable to unmarshal response", err)
	// }

	// v := struct {
	// 	Kind string `json:"kind"`
	// }{}

	// result, err := marshmallow.Unmarshal(data, &v)
	// if err != nil {
	// 	return httperror.InternalServerError("Unable to unmarshal response", err)
	// }

	// if v.Kind == "NamespaceList" {
	// 	result["items"] = []FilteredNamespaceResponse{}
	// }

	//return response.JSON(rw, result)
	rw.Header().Set("Content-Type", "application/json")
	rw.Write(data)
	return nil
}
