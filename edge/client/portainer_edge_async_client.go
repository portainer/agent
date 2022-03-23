package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	portainer "github.com/portainer/portainer/api"

	"github.com/portainer/agent"
	"github.com/portainer/agent/os"
)

// PortainerAsyncClient is used to execute HTTP requests using only the /api/entrypoint/async api endpoint
type PortainerAsyncClient struct {
	httpClient              *http.Client
	serverAddress           string
	endpointID              portainer.EndpointID
	edgeID                  string
	agentPlatformIdentifier agent.ContainerPlatform
	commandTimestamp        *time.Time

	lastAsyncResponse      AsyncResponse
	lastAsyncResponseMutex sync.Mutex
	nextSnapshotRequest    AsyncRequest
	nextSnapshotMutex      sync.Mutex
}

// NewPortainerAsyncClient returns a pointer to a new PortainerAsyncClient instance
func NewPortainerAsyncClient(serverAddress string, endpointID portainer.EndpointID, edgeID string, containerPlatform agent.ContainerPlatform, httpClient *http.Client) *PortainerAsyncClient {
	initialCommandTimestamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return &PortainerAsyncClient{
		serverAddress:           serverAddress,
		endpointID:              endpointID,
		edgeID:                  edgeID,
		httpClient:              httpClient,
		agentPlatformIdentifier: containerPlatform,
		commandTimestamp:        &initialCommandTimestamp,
	}
}

func (client *PortainerAsyncClient) SetTimeout(t time.Duration) {
	client.httpClient.Timeout = t
}

type AsyncRequest struct {
	CommandTimestamp *time.Time `json:"commandTimestamp"`
	Snapshot         snapshot   `json:"snapshot"`
}

type snapshot struct {
	Docker      *portainer.DockerSnapshot
	Kubernetes  *portainer.KubernetesSnapshot
	StackStatus map[portainer.EdgeStackID]portainer.EdgeStackStatus
	// TODO: add job logs payload
}

type JSONPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value"`
}

type AsyncResponse struct {
	PingInterval     time.Duration `json:"pingInterval"`
	SnapshotInterval time.Duration `json:"snapshotInterval"`
	CommandInterval  time.Duration `json:"commandInterval"`

	EndpointID portainer.EndpointID `json:"endpointID"`
	Commands   []AsyncCommand       `json:"commands"`
}

type AsyncCommand struct {
	ID        int         `json:"id"`
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value"`
}

type EdgeStackData struct {
	ID               int
	Version          int
	StackFileContent string
	Name             string
}

func (client *PortainerAsyncClient) GetEnvironmentStatus() (*PollStatusResponse, error) {
	pollURL := fmt.Sprintf("%s/api/endpoints/edge/async", client.serverAddress)

	payload := AsyncRequest{
		CommandTimestamp: client.commandTimestamp,
		Snapshot:         snapshot{},
	}

	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()
	if client.nextSnapshotRequest.Snapshot.StackStatus != nil {
		payload.Snapshot.StackStatus = client.nextSnapshotRequest.Snapshot.StackStatus
		client.nextSnapshotRequest.Snapshot.StackStatus = nil
	}

	/*
		TODO: leaving this commented for faster debugging, should be called only for snapshot request
		switch client.agentPlatformIdentifier {
		case agent.PlatformDocker:
			dockerSnapshot, _ := docker.CreateSnapshot()
			payload.Snapshot = Snapshot{
				Docker: dockerSnapshot,
			}
		case agent.PlatformKubernetes:
			kubernetesSnapshot, _ := kubernetes.CreateSnapshot()
			payload.Snapshot = Snapshot{
				Kubernetes: kubernetesSnapshot,
			}
		}
	*/

	client.lastAsyncResponseMutex.Lock()
	defer client.lastAsyncResponseMutex.Unlock()

	asyncResponse, err := client.executeAsyncRequest(payload, pollURL)
	if err != nil {
		return nil, err
	}
	client.endpointID = asyncResponse.EndpointID

	response := &PollStatusResponse{
		AsyncCommands: asyncResponse.Commands,
	}

	client.lastAsyncResponse = *asyncResponse

	return response, nil
}

func (client *PortainerAsyncClient) executeAsyncRequest(payload AsyncRequest, pollURL string) (*AsyncResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", pollURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatformIdentifier)))

	hostname, err := os.GetHostName()
	if err != nil {
		return nil, err
	}
	req.Header.Set("portainer_hostname", hostname) // TODO use proper header

	log.Printf("[DEBUG] [internal,edge,poll] [message: sending agent platform header] [header: %s]", strconv.Itoa(int(client.agentPlatformIdentifier)))

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[DEBUG] [internal,edge,poll] [response_code: %d] [message: Poll request failure]", resp.StatusCode)
		return nil, errors.New("short poll request failed")
	}

	var asyncResponse AsyncResponse
	err = json.NewDecoder(resp.Body).Decode(&asyncResponse)
	if err != nil {
		return nil, err
	}
	return &asyncResponse, nil
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerAsyncClient) SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if client.nextSnapshotRequest.Snapshot.StackStatus == nil {
		client.nextSnapshotRequest.Snapshot.StackStatus = make(map[portainer.EdgeStackID]portainer.EdgeStackStatus)
	}
	client.nextSnapshotRequest.Snapshot.StackStatus[portainer.EdgeStackID(edgeStackID)] = portainer.EdgeStackStatus{
		EndpointID: client.endpointID,
		Type:       portainer.EdgeStackStatusType(edgeStackStatus),
		Error:      error,
	}

	return nil
}

// SendJobLogFile sends the jobID log to the Portainer server
func (client *PortainerAsyncClient) SendJobLogFile(jobID int, fileContent []byte) error {
	// TODO: This should go into the next snapshot payload
	return nil
}

func (client *PortainerAsyncClient) SetLastCommandTimestamp(timestamp time.Time) {
	client.commandTimestamp = &timestamp
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerAsyncClient) GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error) {
	return nil, nil // unused in async mode
}
