package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"

	"github.com/rs/zerolog/log"
)

// PortainerEdgeClient is used to execute HTTP requests against the Portainer API
type PortainerEdgeClient struct {
	httpClient      *http.Client
	serverAddress   string
	setEndpointIDFn setEndpointIDFn
	getEndpointIDFn getEndpointIDFn
	edgeID          string
	agentPlatform   agent.ContainerPlatform
}

type globalKeyResponse struct {
	EndpointID portainer.EndpointID `json:"endpointID"`
}

// NewPortainerEdgeClient returns a pointer to a new PortainerEdgeClient instance
func NewPortainerEdgeClient(serverAddress string, setEIDFn setEndpointIDFn, getEIDFn getEndpointIDFn, edgeID string, agentPlatform agent.ContainerPlatform, httpClient *http.Client) *PortainerEdgeClient {
	return &PortainerEdgeClient{
		serverAddress:   serverAddress,
		setEndpointIDFn: setEIDFn,
		getEndpointIDFn: getEIDFn,
		edgeID:          edgeID,
		agentPlatform:   agentPlatform,
		httpClient:      httpClient,
	}
}

func (client *PortainerEdgeClient) SetTimeout(t time.Duration) {
	client.httpClient.Timeout = t
}

func (client *PortainerEdgeClient) GetEnvironmentID() (portainer.EndpointID, error) {
	gkURL := fmt.Sprintf("%s/api/endpoints/global-key", client.serverAddress)
	req, err := http.NewRequest(http.MethodPost, gkURL, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Int("response_code", resp.StatusCode).Msg("global key request failure")

		return 0, errors.New("global key request failed")
	}

	var responseData globalKeyResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return 0, err
	}

	return responseData.EndpointID, nil
}

func (client *PortainerEdgeClient) GetEnvironmentStatus(flags ...string) (*PollStatusResponse, error) {
	pollURL := fmt.Sprintf("%s/api/endpoints/%d/edge/status", client.serverAddress, client.getEndpointIDFn())
	req, err := http.NewRequest("GET", pollURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPResponseAgentHeaderName, agent.Version)
	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatform)))

	log.Debug().Int("header", int(client.agentPlatform)).Msg("sending agent platform header")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Int("response_code", resp.StatusCode).Msg("poll request failure]")

		logError(resp)

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
	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/stacks/%d", client.serverAddress, client.getEndpointIDFn(), edgeStackID)

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
		log.Error().Int("response_code", resp.StatusCode).Msg("GetEdgeStackConfig operation failed")

		return nil, errors.New("GetEdgeStackConfig operation failed")
	}

	var data EdgeStackData
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return &agent.EdgeStackConfig{Name: data.Name, FileContent: data.StackFileContent, RegistryCredentials: data.RegistryCredentials}, nil
}

type setEdgeStackStatusPayload struct {
	Error      string
	Status     int
	EndpointID portainer.EndpointID
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerEdgeClient) SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error {
	payload := setEdgeStackStatusPayload{
		Error:      error,
		Status:     edgeStackStatus,
		EndpointID: client.getEndpointIDFn(),
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
		log.Error().Int("response_code", resp.StatusCode).Msg("SetEdgeStackStatus operation failed")

		return errors.New("SetEdgeStackStatus operation failed")
	}

	return nil
}

// DeleteEdgeStackStatus deletes the status of an Edge stack on the Portainer server
func (client *PortainerEdgeClient) DeleteEdgeStackStatus(edgeStackID int) error {
	requestURL := fmt.Sprintf("%s/api/edge_stacks/%d/status/%d", client.serverAddress, edgeStackID, client.getEndpointIDFn())

	req, err := http.NewRequest(http.MethodDelete, requestURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		log.Error().Int("response_code", resp.StatusCode).Msg("DeleteEdgeStackStatus operation failed")

		return errors.New("DeleteEdgeStackStatus operation failed")
	}

	return nil
}

type logFilePayload struct {
	FileContent string
}

// SetEdgeJobStatus sends the jobID log to the Portainer server
func (client *PortainerEdgeClient) SetEdgeJobStatus(edgeJobStatus agent.EdgeJobStatus) error {
	payload := logFilePayload{
		FileContent: edgeJobStatus.LogFileContent,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/jobs/%d/logs", client.serverAddress, client.getEndpointIDFn(), edgeJobStatus.JobID)

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
		log.Error().Int("response_code", resp.StatusCode).Msg("SetEdgeJobStatus operation failed")

		return errors.New("SetEdgeJobStatus operation failed")
	}

	return nil
}

func (client *PortainerEdgeClient) ProcessAsyncCommands() error {
	return nil // edge mode only
}

func (client *PortainerEdgeClient) SetLastCommandTimestamp(timestamp time.Time) {
	return // edge mode only
}

func (client *PortainerEdgeClient) EnqueueLogCollectionForStack(logCmd LogCommandData) error {
	return nil
}
