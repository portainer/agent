package request

import (
	"encoding/json"
	"net/http"

	"bitbucket.org/portainer/agent"
	"github.com/gorilla/mux"
)

const (
	// errInvalidRequestURL defines an error raised when the data sent in the query or the URL is invalid
	errInvalidRequestURL = agent.Error("Invalid request URL")
	// errMissingQueryParameter defines an error raised when a mandatory query parameter is missing.
	errMissingQueryParameter = agent.Error("Missing query parameter")
)

// PayloadValidation is an interface used to validate the payload of a request.
type PayloadValidation interface {
	Validate(request *http.Request) error
}

// DecodeAndValidateJSONPayload decodes the body of the request into an object
// implementing the PayloadValidation interface.
// It also triggers a validation of object content.
func DecodeAndValidateJSONPayload(request *http.Request, v PayloadValidation) error {
	if err := json.NewDecoder(request.Body).Decode(v); err != nil {
		return err
	}
	return v.Validate(request)
}

// RetrieveRouteVariableValue returns the value of a route variable as a string.
func RetrieveRouteVariableValue(request *http.Request, name string) (string, error) {
	routeVariables := mux.Vars(request)
	if routeVariables == nil {
		return "", errInvalidRequestURL
	}
	routeVar := routeVariables[name]
	if routeVar == "" {
		return "", errInvalidRequestURL
	}
	return routeVar, nil
}

// RetrieveQueryParameter returns the value of a query parameter as a string.
// If optional is set to true, will not return an error when the query parameter is not found.
func RetrieveQueryParameter(request *http.Request, name string, optional bool) (string, error) {
	queryParameter := request.FormValue(name)
	if queryParameter == "" && !optional {
		return "", errMissingQueryParameter
	}
	return queryParameter, nil
}
