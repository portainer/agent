package operations

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"bitbucket.org/portainer/agent"
	"github.com/koding/websocketproxy"
)

type parallelRequestResult struct {
	data     []interface{}
	nodeName string
	err      error
}

func executeParallelRequest(request *http.Request, member *agent.ClusterMember, ch chan parallelRequestResult, wg *sync.WaitGroup) {

	response, err := executeRequestOnClusterMember(request, member)
	if err != nil {
		ch <- parallelRequestResult{err: err, data: nil}
		wg.Done()
	}

	sub := ""
	if strings.HasPrefix(request.URL.Path, "/volumes") {
		sub = "Volumes"
	}

	data, err := getResponseAsJSONArray(response, sub)
	if err != nil {
		ch <- parallelRequestResult{err: err, data: nil}
		wg.Done()
	}

	ch <- parallelRequestResult{err: nil, data: data, nodeName: member.NodeName}
	wg.Done()
}

// TODO: try to use a httputil.NewSingleHostReverseProxy here instead of creating a new request
// Might solve the issue related to container console.
// Need to pass responseWriter as a parameter.
func executeRequestOnClusterMember(request *http.Request, member *agent.ClusterMember) (*http.Response, error) {
	// TODO: avoid the usage of ReadAll
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	// you can reassign the body if you need to parse it as multipart
	// TODO: check if this is optional
	request.Body = ioutil.NopCloser(bytes.NewReader(body))

	url := request.URL
	// TODO: member.AgentPort is in the address format here (:9001), could be a real IP address.
	// Fix that.
	url.Host = member.IPAddress + member.Port

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

	// TODO: not sure if a client needs to be instanciated each time we want to proxy a request.
	client := &http.Client{}
	return client.Do(proxyReq)
}

func newSingleHostReverseProxyWithAgentHeader(target *url.URL) *httputil.ReverseProxy {
	targetQuery := target.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = target.Path
		req.Host = req.URL.Host
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		// if _, ok := req.Header["User-Agent"]; !ok {
		// 	// explicitly disable User-Agent so it's not set to default value
		// 	req.Header.Set("User-Agent", "")
		// }
		req.Header.Set("User-Agent", "Docker-Client")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "tcp")
		req.Header.Set(agent.HTTPOperationHeaderName, agent.HTTPOperationHeaderValue)
	}
	return &httputil.ReverseProxy{Director: director}
}

func reverseProxy(rw http.ResponseWriter, request *http.Request, target *url.URL) {
	proxy := newSingleHostReverseProxyWithAgentHeader(target)
	proxy.ServeHTTP(rw, request)
}

func websocketReverseProxy(rw http.ResponseWriter, request *http.Request, target *url.URL) {
	proxy := websocketproxy.NewProxy(target)
	proxy.Director = func(incoming *http.Request, out http.Header) {
		out.Set(agent.HTTPOperationHeaderName, agent.HTTPOperationHeaderValue)
	}
	proxy.ServeHTTP(rw, request)
}
