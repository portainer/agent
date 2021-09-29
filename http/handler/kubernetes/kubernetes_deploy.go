package kubernetes

import (
	"errors"
	"net/http"

	"github.com/portainer/agent"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type (
	deployPayload struct {
		StackConfig string
		Namespace   string
	}

	deployResponse struct {
		Output string
	}
)

func (payload *deployPayload) Validate(r *http.Request) error {
	if payload.StackConfig == "" {
		return errors.New("Missing deployment config")
	}

	return nil
}

func (handler *Handler) kubernetesDeploy(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload deployPayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	token := r.Header.Get(agent.HTTPKubernetesSATokenHeaderName)

	output, err := handler.kubernetesDeployer.DeployRawConfig(token, payload.StackConfig, payload.Namespace)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Failed deploying", err}
	}

	return response.JSON(rw, &deployResponse{Output: string(output)})
}
