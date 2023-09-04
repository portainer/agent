package kubernetesproxy

import (
	"fmt"
	"net/http"
	"os"

	"github.com/portainer/agent"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
)

func (handler *Handler) kubernetesOperation(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	token := request.Header.Get(agent.HTTPKubernetesSATokenHeaderName)
	if token == "" {
		adminToken, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			return httperror.InternalServerError("Unable to read service account token file", err)
		}

		token = string(adminToken)
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	http.StripPrefix("/kubernetes", handler.kubernetesProxy).ServeHTTP(rw, request)
	return nil
}
