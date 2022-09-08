package sosivio

import (
	"io/ioutil"
	"net/http"

	httperror "github.com/portainer/libhttp/error"
)

// TODO: REVIEW-POC-SOSIVIO
// Port to libhttp.
// Raw returns raw data. Returns a pointer to a
// HandlerError if encoding fails.
func Raw(rw http.ResponseWriter, data []byte) *httperror.HandlerError {
	rw.Write(data)
	return nil
}

// GET request on /sosivio/namespaces
func (handler *Handler) namespaces(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

	// TODO: REVIEW-POC-SOSIVIO
	// Make use of a proper HTTP client here to manage timeouts
	// Alternatively, a proxy can probably be used to handle ALL Sosivio related requests.
	resp, err := http.Get("http://poc-api.portainer.svc.cluster.local:8088/api/v1/namespaces")
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to query Sosivio API", err}
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to parse Sosivio API response", err}
	}

	return Raw(rw, body)
}
