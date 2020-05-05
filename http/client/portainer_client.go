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

// PortainerClient is used to execute HTTP requests against the agent API
type PortainerClient struct {
	httpClient    *http.Client
	serverAddress string
	endpointID    string
	edgeID        string
}

// NewPortainerClient returns a pointer to a new PortainerClient instance
func NewPortainerClient(serverAddress, endpointID, edgeID string) *PortainerClient {
	return &PortainerClient{
		serverAddress: serverAddress,
		endpointID:    endpointID,
		edgeID:        edgeID,
		httpClient: &http.Client{
			Timeout: time.Second * 3,
		},
	}
}

type stackConfigResponse struct {
	StackFileContent string
	Prune            bool
}

// GetEdgeStackConfig fetches the Edge stack config
func (client *PortainerClient) GetEdgeStackConfig(edgeStackID int) (string, bool, error) {
	requestURL := fmt.Sprintf("%s/api/endpoints/%s/edge/stacks/%d", client.serverAddress, client.endpointID, edgeStackID)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return "", false, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ERROR] [http,client,portainer] [response_code: %d] [message: GetEdgeStackConfig operation failed] \n", resp.StatusCode)
		return "", false, errors.New("GetEdgeStackConfig operation failed")
	}

	var data stackConfigResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return "", false, err
	}

	return data.StackFileContent, data.Prune, nil
}

type setEdgeStackStatusPayload struct {
	Error      string
	Status     int
	EndpointID int
}

// SetEdgeStackStatus set the Edge stack status on portainer server
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
		log.Printf("[ERROR] [http,client] [response_code: %d] [message: SetEdgeStackStatus operation failed]", resp.StatusCode)
		return errors.New("SetEdgeStackStatus operation failed")
	}

	return nil
}
