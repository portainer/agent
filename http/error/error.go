package error

import (
	"encoding/json"
	"log"
	"net/http"

	"bitbucket.org/portainer/agent"
)

// errorResponse is a generic response for sending a error.
type errorResponse struct {
	Err string `json:"err,omitempty"`
}

// WriteErrorResponse writes an error message to the response and logger.
func WriteErrorResponse(rw http.ResponseWriter, err error, code int, logger *log.Logger) {
	if logger != nil {
		logger.Printf("http error: %s (code=%d)", err, code)
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set(agent.HTTPResponseAgentHeaderName, agent.AgentVersion)
	rw.WriteHeader(code)
	json.NewEncoder(rw).Encode(&errorResponse{Err: err.Error()})
}
