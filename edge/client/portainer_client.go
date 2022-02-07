package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/portainer/agent"
)

// PortainerClient is used to execute HTTP requests against the Portainer API
type PortainerClient struct {
	httpClient    *http.Client
	serverAddress string
	endpointID    string
	edgeID        string
}

// NewPortainerClient returns a pointer to a new PortainerClient instance
func NewPortainerClient(serverAddress, endpointID, edgeID string, httpClient *http.Client) *PortainerClient {
	return &PortainerClient{
		serverAddress: serverAddress,
		endpointID:    endpointID,
		edgeID:        edgeID,
		httpClient:    httpClient, //GetNewHttpClient(10, insecurePoll),
	}
}

type stackConfigResponse struct {
	Name             string
	StackFileContent string
	ImageMapping     map[string]string // a map of stackfile image to imageCache url(with sha)
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerClient) GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error) {
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
		log.Printf("[ERROR] [http,client,portainer] [response_code: %d] [message: GetEdgeStackConfig operation failed]", resp.StatusCode)
		return nil, errors.New("GetEdgeStackConfig operation failed")
	}

	var data stackConfigResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return &agent.EdgeStackConfig{Name: data.Name, FileContent: data.StackFileContent, ImageMapping: data.ImageMapping}, nil
}

type setEdgeStackStatusPayload struct {
	Error      string
	Status     int
	EndpointID int
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerClient) SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error {
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
		log.Printf("[ERROR] [http,client,portainer] [response_code: %d] [message: SetEdgeStackStatus operation failed]", resp.StatusCode)
		return errors.New("SetEdgeStackStatus operation failed")
	}

	return nil
}

type logFilePayload struct {
	FileContent string
}

// SendJobLogFile sends the jobID log to the Portainer server
func (client *PortainerClient) SendJobLogFile(jobID int, fileContent []byte) error {
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
		log.Printf("[ERROR] [http,client,portainer] [response_code: %d] [message: SendJobLogFile operation failed]", resp.StatusCode)
		return errors.New("SendJobLogFile operation failed")
	}

	return nil

}
