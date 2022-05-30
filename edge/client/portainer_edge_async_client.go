package client

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/os"
	portainer "github.com/portainer/portainer/api"
	"github.com/wI2L/jsondiff"
)

// PortainerAsyncClient is used to execute HTTP requests using only the /api/entrypoint/async api endpoint
type PortainerAsyncClient struct {
	httpClient              *http.Client
	serverAddress           string
	setEndpointIDFn         setEndpointIDFn
	getEndpointIDFn         getEndpointIDFn
	edgeID                  string
	agentPlatformIdentifier agent.ContainerPlatform
	commandTimestamp        *time.Time

	lastAsyncResponse      AsyncResponse
	lastAsyncResponseMutex sync.Mutex
	lastSnapshot           snapshot
	nextSnapshot           snapshot
	nextSnapshotMutex      sync.Mutex

	stackLogCollectionQueue []LogCommandData
}

// NewPortainerAsyncClient returns a pointer to a new PortainerAsyncClient instance
func NewPortainerAsyncClient(serverAddress string, setEIDFn setEndpointIDFn, getEIDFn getEndpointIDFn, edgeID string, containerPlatform agent.ContainerPlatform, httpClient *http.Client) *PortainerAsyncClient {
	initialCommandTimestamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return &PortainerAsyncClient{
		serverAddress:           serverAddress,
		setEndpointIDFn:         setEIDFn,
		getEndpointIDFn:         getEIDFn,
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
	CommandTimestamp *time.Time           `json:"commandTimestamp,omitempty"`
	Snapshot         *snapshot            `json:"snapshot,omitempty"`
	EndpointId       portainer.EndpointID `json:"endpointId,omitempty"`
}

type EndpointLog struct {
	DockerContainerID string `json:"dockerContainerID,omitempty"`
	StdOut            string `json:"stdOut,omitempty"`
	StdErr            string `json:"stdErr,omitempty"`
}

type EdgeStackLog struct {
	EdgeStackID portainer.EdgeStackID `json:"edgeStackID,omitempty"`
	Logs        []EndpointLog         `json:"logs,omitempty"`
}

type snapshot struct {
	Docker          *portainer.DockerSnapshot                           `json:"docker,omitempty"`
	DockerPatch     jsondiff.Patch                                      `json:"dockerPatch,omitempty"`
	Kubernetes      *portainer.KubernetesSnapshot                       `json:"kubernetes,omitempty"`
	KubernetesPatch jsondiff.Patch                                      `json:"kubernetesPatch,omitempty"`
	StackLogs       []EdgeStackLog                                      `json:"stackLogs,omitempty"`
	StackStatus     map[portainer.EdgeStackID]portainer.EdgeStackStatus `json:"stackStatus,omitempty"`
	JobsStatus      map[portainer.EdgeJobID]agent.EdgeJobStatus         `json:"jobsStatus:,omitempty"`
}

type AsyncResponse struct {
	PingInterval     time.Duration `json:"pingInterval"`
	SnapshotInterval time.Duration `json:"snapshotInterval"`
	CommandInterval  time.Duration `json:"commandInterval"`

	EndpointID portainer.EndpointID `json:"endpointID"`
	Commands   []AsyncCommand       `json:"commands"`
}

type AsyncCommand struct {
	ID         int                  `json:"id"`
	Type       string               `json:"type"`
	EndpointID portainer.EndpointID `json:"endpointID"`
	Timestamp  time.Time            `json:"timestamp"`
	Operation  string               `json:"op"`
	Path       string               `json:"path"`
	Value      interface{}          `json:"value"`
}

type EdgeStackData struct {
	ID                  int
	Version             int
	Name                string
	StackFileContent    string
	RegistryCredentials []agent.RegistryCredentials
}

type EdgeJobData struct {
	ID                portainer.EdgeJobID
	CollectLogs       bool
	LogsStatus        portainer.EdgeJobLogsStatus
	CronExpression    string
	ScriptFileContent string
	Version           int
}

type LogCommandData struct {
	EdgeStackID   portainer.EdgeStackID
	EdgeStackName string
	Tail          int
}

func (client *PortainerAsyncClient) GetEnvironmentID() (portainer.EndpointID, error) {
	return 0, errors.New("GetEnvironmentID is not available in async mode")
}

func (client *PortainerAsyncClient) GetEnvironmentStatus(flags ...string) (*PollStatusResponse, error) {
	pollURL := fmt.Sprintf("%s/api/endpoints/edge/async", client.serverAddress)

	payload := AsyncRequest{}
	payload.EndpointId = client.getEndpointIDFn()

	var doSnapshot, doCommand bool
	for _, f := range flags {
		if f == "snapshot" {
			doSnapshot = true
		} else if f == "command" {
			doCommand = true
		}
	}

	var currentSnapshot snapshot
	if doSnapshot {
		payload.Snapshot = &snapshot{}

		switch client.agentPlatformIdentifier {
		case agent.PlatformDocker:
			dockerSnapshot, err := docker.CreateSnapshot()
			if err != nil {
				log.Printf("[WARN] [edge,client] [message: could not create the Docker snapshot: %s]", err)
			}

			payload.Snapshot.Docker = dockerSnapshot
			currentSnapshot.Docker = dockerSnapshot

			if client.lastSnapshot.Docker != nil {
				dockerPatch, err := jsondiff.Compare(client.lastSnapshot.Docker, dockerSnapshot)
				if err == nil {
					payload.Snapshot.DockerPatch = dockerPatch
					payload.Snapshot.Docker = nil
				} else {
					log.Printf("[WARN] [edge,client] [message: could not generate the Docker snapshot patch: %s]", err)
				}
			}

			for _, stack := range client.stackLogCollectionQueue {
				cs, err := docker.GetContainersWithLabel(fmt.Sprintf("com.docker.compose.project=edge_%s", stack.EdgeStackName))
				if err != nil {
					log.Printf("[WARN] [edge,client] [message: could not retrieve containers for stack '%s': %s]", stack.EdgeStackName, err)
					continue
				}

				edgeStackLog := EdgeStackLog{
					EdgeStackID: stack.EdgeStackID,
				}

				for _, c := range cs {
					stdOut, stdErr, err := docker.GetContainerLogs(c.ID, strconv.Itoa(stack.Tail))
					if err != nil {
						log.Printf("[WARN] [edge,client] [message: could not retrieve logs for container '%s': %s]", c.ID, err)
						continue
					}

					edgeStackLog.Logs = append(edgeStackLog.Logs, EndpointLog{
						DockerContainerID: c.ID,
						StdOut:            string(stdOut),
						StdErr:            string(stdErr),
					})
				}

				if len(edgeStackLog.Logs) > 0 {
					payload.Snapshot.StackLogs = append(payload.Snapshot.StackLogs, edgeStackLog)
				}
			}

		case agent.PlatformKubernetes:
			kubeSnapshot, err := kubernetes.CreateSnapshot()
			if err != nil {
				log.Printf("[WARN] [edge,client] [message: could not create the Kubernetes snapshot: %s]", err)
			}

			payload.Snapshot.Kubernetes = kubeSnapshot
			currentSnapshot.Kubernetes = kubeSnapshot

			if client.lastSnapshot.Kubernetes != nil {
				kubePatch, err := jsondiff.Compare(client.lastSnapshot.Docker, kubeSnapshot)
				if err == nil {
					payload.Snapshot.KubernetesPatch = kubePatch
					payload.Snapshot.KubernetesPatch = nil
				} else {
					log.Printf("[WARN] [edge,client] [message: could not generate the Kubernetes snapshot patch: %s]", err)
				}
			}
		}

		client.nextSnapshotMutex.Lock()
		defer client.nextSnapshotMutex.Unlock()

		payload.Snapshot.StackStatus = client.nextSnapshot.StackStatus
		payload.Snapshot.JobsStatus = client.nextSnapshot.JobsStatus
	}

	if doCommand {
		payload.CommandTimestamp = client.commandTimestamp
	}

	client.lastAsyncResponseMutex.Lock()
	defer client.lastAsyncResponseMutex.Unlock()

	asyncResponse, err := client.executeAsyncRequest(payload, pollURL)
	if err != nil {
		return nil, err
	}

	if doSnapshot {
		client.lastSnapshot.Docker = currentSnapshot.Docker
		client.lastSnapshot.Kubernetes = currentSnapshot.Kubernetes

		client.nextSnapshot.StackStatus = nil
		client.nextSnapshot.JobsStatus = nil

		client.stackLogCollectionQueue = nil
	}

	client.setEndpointIDFn(asyncResponse.EndpointID)

	response := &PollStatusResponse{
		AsyncCommands:    asyncResponse.Commands,
		PingInterval:     asyncResponse.PingInterval,
		SnapshotInterval: asyncResponse.SnapshotInterval,
		CommandInterval:  asyncResponse.CommandInterval,
	}

	client.lastAsyncResponse = *asyncResponse

	return response, nil
}

func gzipCompress(data []byte) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}

	gz, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}

	_, err = gz.Write(data)
	if err != nil {
		return nil, err
	}

	err = gz.Close()
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func (client *PortainerAsyncClient) executeAsyncRequest(payload AsyncRequest, pollURL string) (*AsyncResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var buf *bytes.Buffer
	if payload.Snapshot != nil {
		buf, err = gzipCompress(data)
		if err != nil {
			return nil, err
		}
	} else {
		buf = bytes.NewBuffer(data)
	}

	req, err := http.NewRequest("POST", pollURL, buf)
	if err != nil {
		return nil, err
	}

	if payload.Snapshot != nil {
		req.Header.Set("Content-Encoding", "gzip")
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

	if client.nextSnapshot.StackStatus == nil {
		client.nextSnapshot.StackStatus = make(map[portainer.EdgeStackID]portainer.EdgeStackStatus)
	}

	client.nextSnapshot.StackStatus[portainer.EdgeStackID(edgeStackID)] = portainer.EdgeStackStatus{
		EndpointID: client.getEndpointIDFn(),
		Type:       portainer.EdgeStackStatusType(edgeStackStatus),
		Error:      error,
	}

	return nil
}

// SetEdgeJobStatus sends the jobID log to the Portainer server
func (client *PortainerAsyncClient) SetEdgeJobStatus(edgeJobStatus agent.EdgeJobStatus) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if client.nextSnapshot.JobsStatus == nil {
		client.nextSnapshot.JobsStatus = make(map[portainer.EdgeJobID]agent.EdgeJobStatus)
	}

	client.nextSnapshot.JobsStatus[portainer.EdgeJobID(edgeJobStatus.JobID)] = edgeJobStatus

	return nil
}

func (client *PortainerAsyncClient) SetLastCommandTimestamp(timestamp time.Time) {
	client.commandTimestamp = &timestamp
}

func (client *PortainerAsyncClient) DeleteEdgeStackStatus(edgeStackID int) error {
	return nil // unused in async mode
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerAsyncClient) GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error) {
	return nil, nil // unused in async mode
}

func (client *PortainerAsyncClient) EnqueueLogCollectionForStack(logCmd LogCommandData) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	client.stackLogCollectionQueue = append(client.stackLogCollectionQueue, logCmd)

	return nil
}

