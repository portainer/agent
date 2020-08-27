package kubernetes

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
)

func (handler *Handler) kubernetesOperation(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	token := request.Header.Get(agent.HTTPKubernetesSATokenHeaderName)
	if token == "" {
		adminToken, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			return &httperror.HandlerError{http.StatusInternalServerError, "Unable to read service account token file", err}
		}

		token = string(adminToken)
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	http.StripPrefix("/kubernetes", handler.kubernetesProxy).ServeHTTP(rw, request)
	return nil
}
