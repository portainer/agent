package nomadproxy

import (
	"github.com/portainer/agent"
	"net/http"

	httperror "github.com/portainer/libhttp/error"
)

func (handler *Handler) nomadOperation(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	request.Header.Set(agent.HTTPNomadTokenHeaderName, handler.nomadConfig.NomadToken)
	http.StripPrefix("/nomad", handler.nomadProxy).ServeHTTP(rw, request)
	return nil
}
