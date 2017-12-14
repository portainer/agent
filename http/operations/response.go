package operations

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"bitbucket.org/portainer/agent"
)

const (
	errEmptyResponseBody = agent.Error("Empty response body")
)

func getResponseAsJSONArray(response *http.Response) ([]interface{}, error) {
	responseData, err := getResponseBodyAsGenericJSON(response)
	if err != nil {
		return nil, err
	}

	responseObject := responseData.([]interface{})
	return responseObject, nil
}

func getResponseBodyAsGenericJSON(response *http.Response) (interface{}, error) {
	var data interface{}
	if response.Body != nil {
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}

		err = response.Body.Close()
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(body, &data)
		if err != nil {
			return nil, err
		}

		return data, nil
	}
	return nil, errEmptyResponseBody
}
