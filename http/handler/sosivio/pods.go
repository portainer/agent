package sosivio

import (
	"io/ioutil"
	"net/http"

	httperror "github.com/portainer/libhttp/error"
)

// GET request on /sosivio/pods
func (handler *Handler) pods(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

	// TODO: REVIEW-POC-SOSIVIO
	// Make use of a proper HTTP client here to manage timeouts
	// Alternatively, a proxy can probably be used to handle ALL Sosivio related requests.
	resp, err := http.Get("http://poc-api.portainer.svc.cluster.local:8088/api/v1/pod?" + r.URL.RawQuery)
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
