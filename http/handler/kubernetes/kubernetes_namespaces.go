package kubernetes

import (
	"fmt"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
	"github.com/rs/zerolog/log"
)

// type getNamespacePayload struct{}

// func (payload *getNamespacePayload) Validate(r *http.Request) error {

// 	return nil
// }

func (handler *Handler) kubernetesGetNamespaces(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

	log.Debug().Msgf("GetNamespaces Handler: Request: %s %s", r.Method, r.URL.Path)

	// var payload getNamespacePayload
	// err := request.DecodeAndValidateJSONPayload(r, &payload)
	// if err != nil {
	// 	return httperror.BadRequest("Invalid request payload", err)
	// }

	for _, header := range r.Header {
		for _, value := range header {
			fmt.Println("Header:", value)
		}
	}

	return response.Empty(rw)
}
