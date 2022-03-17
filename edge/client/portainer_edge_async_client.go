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
	endpointID              string
	edgeID                  string
	agentPlatformIdentifier agent.ContainerPlatform
	commandTimestamp        *time.Time

	lastAsyncResponse      AsyncResponse
	lastAsyncResponseMutex sync.Mutex
	nextSnapshotRequest    AsyncRequest
	nextSnapshotMutex      sync.Mutex
}

// NewPortainerAsyncClient returns a pointer to a new PortainerAsyncClient instance
func NewPortainerAsyncClient(serverAddress, endpointID, edgeID string, containerPlatform agent.ContainerPlatform, httpClient *http.Client) *PortainerAsyncClient {
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

type edgeStackData struct {
	ID               int
	Version          int
	StackFileContent string
	Name             string
}

type AsyncRequest struct {
	CommandTimestamp *time.Time `json:"commandTimestamp"`
	Snapshot         snapshot   `json:"snapshot"`
}

type snapshot struct {
	Docker      *portainer.DockerSnapshot
	Kubernetes  *portainer.KubernetesSnapshot
	StackStatus map[portainer.EdgeStackID]portainer.EdgeStackStatus
	// TODO add job logs
}

type JSONPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value"`
}

type AsyncResponse struct {
	PingInterval     string `json:"pingInterval"`
	SnapshotInterval string `json:"snapshotInterval"`
	CommandInterval  string `json:"commandInterval"`

	ServerCommandId      string         // should be easy to detect if its larger / smaller:  this is the response that tells the agent there are new commands waiting for it
	SendDiffSnapshotTime time.Time      `json: optional` // might be optional
	Commands             []AsyncCommand `json:"commands"`
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
	// end Snapshot

	client.lastAsyncResponseMutex.Lock()
	defer client.lastAsyncResponseMutex.Unlock()

	asyncResponse, err := client.executeAsyncRequest(payload, pollURL)
	if err != nil {
		return nil, err
	}

	response := &PollStatusResponse{
		AsyncCommands:   asyncResponse.Commands,
		Status:          "NOTUNNEL", // TODO delete?
		CheckinInterval: -1,         // TODO delete?
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

// TODO borrar todo de aca para abajo???
// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerAsyncClient) GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error) {
	client.lastAsyncResponseMutex.Lock()
	defer client.lastAsyncResponseMutex.Unlock()

	log.Printf("[ERROR] [http,client,portainer] GetEdgeStackConfig(%d) not found", edgeStackID)

	return nil, fmt.Errorf("GetEdgeStackConfig(%d) not found", edgeStackID)
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server //TODO change comment for async mode
func (client *PortainerAsyncClient) SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error {
	// This should go into the next snapshot payload
	endpointID, err := strconv.Atoi(client.endpointID)
	if err != nil {
		return err
	}

	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()
	if client.nextSnapshotRequest.Snapshot.StackStatus == nil {
		client.nextSnapshotRequest.Snapshot.StackStatus = make(map[portainer.EdgeStackID]portainer.EdgeStackStatus)
	}
	client.nextSnapshotRequest.Snapshot.StackStatus[portainer.EdgeStackID(edgeStackID)] = portainer.EdgeStackStatus{
		Error:      error,
		Type:       portainer.EdgeStackStatusType(edgeStackStatus),
		EndpointID: portainer.EndpointID(endpointID),
	}

	return nil
}

// SendJobLogFile sends the jobID log to the Portainer server
func (client *PortainerAsyncClient) SendJobLogFile(jobID int, fileContent []byte) error {
	// This should go into the next snapshot payload
	return nil

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

func (client *PortainerAsyncClient) SetLastCommandTimestamp(timestamp time.Time) {
	client.commandTimestamp = &timestamp
}
