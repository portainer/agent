package kubernetes

import (
	"fmt"
	"io/ioutil"
	"net/http"

	httperror "github.com/portainer/libhttp/error"
)

func (handler *Handler) kubernetesOperation(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	token, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to read service account token file", err}
	}
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	http.StripPrefix("/kubernetes", handler.kubernetesProxy).ServeHTTP(rw, request)
	return nil
}
