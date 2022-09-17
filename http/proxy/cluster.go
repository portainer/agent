package proxy

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/portainer/agent"

	"github.com/rs/zerolog/log"
)

const defaultClusterRequestTimeout = 120

// ClusterProxy is a service used to execute the same requests on multiple targets.
type ClusterProxy struct {
	client     *http.Client
	pingClient *http.Client
	useTLS     bool
}

// NewClusterProxy returns a pointer to a ClusterProxy.
// It also sets the default values used in the underlying http.Client.
func NewClusterProxy(useTLS bool) *ClusterProxy {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	return &ClusterProxy{
		client: &http.Client{
			Timeout: time.Second * defaultClusterRequestTimeout,
			Transport: &http.Transport{
				TLSClientConfig:   tlsConfig,
				DisableKeepAlives: true,
			},
		},
		pingClient: &http.Client{
			Timeout: time.Second * 3,
			Transport: &http.Transport{
				TLSClientConfig:   tlsConfig,
				DisableKeepAlives: true,
			},
		},
		useTLS: useTLS,
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
			log.Warn().
				Str("node", result.nodeName).
				Err(result.err).
				Msg("unable to retrieve node resources for aggregation")

			continue
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

func (clusterProxy *ClusterProxy) pingAgent(request *http.Request, member *agent.ClusterMember) error {
	agentScheme := "http"
	if request.TLS != nil {
		agentScheme = "https"
	}

	agentURL := fmt.Sprintf("%s://%s:%s/ping", agentScheme, member.IPAddress, member.Port)

	pingRequest, err := http.NewRequest(http.MethodGet, agentURL, nil)
	if err != nil {
		return err
	}

	response, err := clusterProxy.pingClient.Do(pingRequest)
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusNoContent {
		return errors.New("agent ping request failed")
	}

	return nil
}

func (clusterProxy *ClusterProxy) copyAndExecuteRequest(request *http.Request, member *agent.ClusterMember, ch chan agentRequestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	err := clusterProxy.pingAgent(request, member)
	if err != nil {
		ch <- agentRequestResult{err: err, nodeName: member.NodeName}
		return
	}

	requestCopy, err := copyRequest(request, member, clusterProxy.useTLS)
	if err != nil {
		ch <- agentRequestResult{err: err, nodeName: member.NodeName}
		return
	}

	response, err := clusterProxy.client.Do(requestCopy)
	if err != nil {
		ch <- agentRequestResult{err: err, nodeName: member.NodeName}
		return
	}
	defer response.Body.Close()

	data, err := responseToJSONArray(response, request.URL.Path)
	if err != nil {
		ch <- agentRequestResult{err: err, nodeName: member.NodeName}
		return
	}

	ch <- agentRequestResult{err: nil, responseContent: data, nodeName: member.NodeName}
}

func copyRequest(request *http.Request, member *agent.ClusterMember, useTLS bool) (*http.Request, error) {
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	url := request.URL
	url.Host = member.IPAddress + ":" + member.Port

	url.Scheme = "http"
	if useTLS {
		url.Scheme = "https"
	}

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
	metadata := agent.Metadata{}
	metadata.Agent.NodeName = nodeName

	JSONObject := object.(map[string]interface{})
	JSONObject[agent.ResponseMetadataKey] = metadata

	return JSONObject
}
