package kubernetes

import (
	"fmt"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type getNamespacePayload struct{}

func (payload *getNamespacePayload) Validate(r *http.Request) error {

	return nil
}

func (handler *Handler) kubernetesGetNamespaces(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload getNamespacePayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}

	for _, header := range r.Header {
		for _, value := range header {
			fmt.Println("Header:", value)
		}
	}

	return response.Empty(rw)
}
