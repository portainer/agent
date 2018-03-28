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

func getResponseAsJSONArray(response *http.Response, sub string) ([]interface{}, error) {
	responseData, err := getResponseBodyAsGenericJSON(response)
	if err != nil {
		return nil, err
	}

	var responseObject []interface{}
	if sub != "" {
		obj := responseData.(map[string]interface{})
		if obj[sub] != nil {
			responseObject = obj[sub].([]interface{})
		} else {
			responseObject = make([]interface{}, 0)
		}
	} else {
		responseObject = responseData.([]interface{})
	}

	return responseObject, nil
}

func getResponseBodyAsGenericJSON(response *http.Response) (interface{}, error) {
	var data interface{}
	if response.Body != nil {
		// TODO: avoid the usage of ReadAll
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
