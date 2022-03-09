package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/portainer/agent"
)

// PortainerEdgeClient is used to execute HTTP requests against the Portainer API
type PortainerEdgeClient struct {
	httpClient    *http.Client
	serverAddress string
	endpointID    string
	edgeID        string
	agentPlatform agent.ContainerPlatform
}

// NewPortainerEdgeClient returns a pointer to a new PortainerEdgeClient instance
func NewPortainerEdgeClient(serverAddress, endpointID, edgeID string, agentPlatform agent.ContainerPlatform, httpClient *http.Client) *PortainerEdgeClient {
	return &PortainerEdgeClient{
		serverAddress: serverAddress,
		endpointID:    endpointID,
		edgeID:        edgeID,
		agentPlatform: agentPlatform,
		httpClient:    httpClient,
	}
}

func (client *PortainerEdgeClient) SetTimeout(t time.Duration) {
	client.httpClient.Timeout = t
}

func (client *PortainerEdgeClient) GetEnvironmentStatus() (*PollStatusResponse, error) {
	pollURL := fmt.Sprintf("%s/api/endpoints/%s/edge/status", client.serverAddress, client.endpointID)
	req, err := http.NewRequest("GET", pollURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatform)))

	log.Printf("[DEBUG] [internal,edge,poll] [message: sending agent platform header] [header: %s]", strconv.Itoa(int(client.agentPlatform)))

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[DEBUG] [internal,edge,poll] [response_code: %d] [message: Poll request failure]", resp.StatusCode)
		return nil, errors.New("short poll request failed")
	}

	var responseData PollStatusResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return nil, err
	}
	return &responseData, nil
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerEdgeClient) GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error) {
	requestURL := fmt.Sprintf("%s/api/endpoints/%s/edge/stacks/%d", client.serverAddress, client.endpointID, edgeStackID)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ERROR] [http,client] [response_code: %d] [message: GetEdgeStackConfig operation failed]", resp.StatusCode)
		return nil, errors.New("GetEdgeStackConfig operation failed")
	}

	var data stackConfigResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return &agent.EdgeStackConfig{Name: data.Name, FileContent: data.StackFileContent}, nil
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerEdgeClient) SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error {
	endpointID, err := strconv.Atoi(client.endpointID)
	if err != nil {
		return err
	}

	payload := setEdgeStackStatusPayload{
		Error:      error,
		Status:     edgeStackStatus,
		EndpointID: endpointID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/edge_stacks/%d/status", client.serverAddress, edgeStackID)

	req, err := http.NewRequest(http.MethodPut, requestURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ERROR] [http,client] [response_code: %d] [message: SetEdgeStackStatus operation failed]", resp.StatusCode)
		return errors.New("SetEdgeStackStatus operation failed")
	}

	return nil
}

type logFilePayload struct {
	FileContent string
}

// SendJobLogFile sends the jobID log to the Portainer server
func (client *PortainerEdgeClient) SendJobLogFile(jobID int, fileContent []byte) error {
	payload := logFilePayload{
		FileContent: string(fileContent),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/endpoints/%s/edge/jobs/%d/logs", client.serverAddress, client.endpointID, jobID)

	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ERROR] [http,client] [response_code: %d] [message: SendJobLogFile operation failed]", resp.StatusCode)
		return errors.New("SendJobLogFile operation failed")
	}

	return nil
}
