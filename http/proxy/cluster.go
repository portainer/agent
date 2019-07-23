package proxy

import (
	"bytes"
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/portainer/agent"
)

const defaultClusterRequestTimeout = 60

// ClusterProxy is a service used to execute the same requests on multiple targets.
type ClusterProxy struct {
	client *http.Client
}

// NewClusterProxy returns a pointer to a ClusterProxy.
// It also sets the default values used in the underlying http.Client.
func NewClusterProxy() *ClusterProxy {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	return &ClusterProxy{
		client: &http.Client{
			Timeout: time.Second * defaultClusterRequestTimeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

type agentRequestResult struct {
	responseContent []interface{}
	err             error
	nodeName        string
}

// ClusterOperation will copy and execute the specified request on a set of agents.
// It aggregates the data of each request's response in a single response object.
func (clusterProxy *ClusterProxy) ClusterOperation(request *http.Request, clusterMembers []agent.ClusterMember) (interface{}, error) {

	memberCount := len(clusterMembers)

	dataChannel := make(chan agentRequestResult, memberCount)

	clusterProxy.executeRequestOnCluster(request, clusterMembers, dataChannel)

	close(dataChannel)

	aggregatedData := make([]interface{}, 0, memberCount)

	for result := range dataChannel {
		if result.err != nil {
			return nil, result.err
		}

		for _, item := range result.responseContent {
			decoratedObject := decorateObject(item, result.nodeName)
			aggregatedData = append(aggregatedData, decoratedObject)
		}
	}

	responseData := reproduceDockerAPIResponse(aggregatedData, request.URL.Path)

	return responseData, nil
}

func (clusterProxy *ClusterProxy) executeRequestOnCluster(request *http.Request, clusterMembers []agent.ClusterMember, ch chan agentRequestResult) {

	wg := &sync.WaitGroup{}

	for i := range clusterMembers {
		wg.Add(1)
		member := clusterMembers[i]
		go clusterProxy.copyAndExecuteRequest(request, &member, ch, wg)
	}

	wg.Wait()
}

func (clusterProxy *ClusterProxy) copyAndExecuteRequest(request *http.Request, member *agent.ClusterMember, ch chan agentRequestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	requestCopy, err := copyRequest(request, member)
	if err != nil {
		ch <- agentRequestResult{err: err}
		return
	}

	response, err := clusterProxy.client.Do(requestCopy)
	if err != nil {
		ch <- agentRequestResult{err: err}
		return
	}
	defer response.Body.Close()

	data, err := responseToJSONArray(response, request.URL.Path)
	if err != nil {
		ch <- agentRequestResult{err: err}
		return
	}

	ch <- agentRequestResult{err: nil, responseContent: data, nodeName: member.NodeName}
}

func copyRequest(request *http.Request, member *agent.ClusterMember) (*http.Request, error) {
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	url := request.URL
	url.Host = member.IPAddress + ":" + member.Port
	url.Scheme = "https"

	requestCopy, err := http.NewRequest(request.Method, url.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	requestCopy.Header = cloneHeader(request.Header)
	requestCopy.Header.Set(agent.HTTPTargetHeaderName, member.NodeName)
	return requestCopy, nil
}

func cloneHeader(h http.Header) http.Header {
	h2 := make(http.Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

func decorateObject(object interface{}, nodeName string) interface{} {
	metadata := agent.AgentMetadata{}
	metadata.Agent.NodeName = nodeName

	JSONObject := object.(map[string]interface{})
	JSONObject[agent.ResponseMetadataKey] = metadata

	return JSONObject
}
