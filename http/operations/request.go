package operations

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"sync"

	"bitbucket.org/portainer/agent"
)

type parallelRequestResult struct {
	data     []interface{}
	nodeName string
	err      error
}

func executeParallelRequest(request *http.Request, host string, ch chan parallelRequestResult, wg *sync.WaitGroup) {

	response, err := executeRequestOnSpecifiedHost(request, host)
	if err != nil {
		ch <- parallelRequestResult{err: err, data: nil}
		wg.Done()
	}

	data, err := getResponseAsJSONArray(response)
	if err != nil {
		ch <- parallelRequestResult{err: err, data: nil}
		wg.Done()
	}

	ch <- parallelRequestResult{err: nil, data: data, nodeName: host}
	wg.Done()
}

func executeRequestOnSpecifiedHost(request *http.Request, host string) (*http.Response, error) {
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	// you can reassign the body if you need to parse it as multipart
	// TODO: check if this is optional
	request.Body = ioutil.NopCloser(bytes.NewReader(body))

	url := request.URL
	// TODO: retrieve port from agent metadata
	url.Host = host + ":9001"

	// TODO: figure out if this is the best way to determine scheme
	url.Scheme = "http"
	if request.TLS != nil {
		url.Scheme = "https"
	}

	proxyReq, err := http.NewRequest(request.Method, url.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	proxyReq.Header = request.Header
	proxyReq.Header.Set(agent.HTTPOperationHeaderName, agent.HTTPOperationHeaderValue)

	client := &http.Client{}
	return client.Do(proxyReq)
}
