package client

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/kubernetes"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	"github.com/rs/zerolog/log"
	"github.com/wI2L/jsondiff"
)

// PortainerAsyncClient is used to execute HTTP requests using only the /api/entrypoint/async api endpoint
type PortainerAsyncClient struct {
	httpClient              *edgeHTTPClient
	serverAddress           string
	setEndpointIDFn         setEndpointIDFn
	getEndpointIDFn         getEndpointIDFn
	edgeID                  string
	agentPlatformIdentifier agent.ContainerPlatform
	commandTimestamp        *time.Time
	metaFields              agent.EdgeMetaFields

	lastAsyncResponse AsyncResponse
	lastSnapshot      snapshot
	nextSnapshot      snapshot
	nextSnapshotMutex sync.Mutex
	snapshotRetried   bool

	stackLogCollectionQueue []LogCommandData
}

// NewPortainerAsyncClient returns a pointer to a new PortainerAsyncClient instance
func NewPortainerAsyncClient(serverAddress string, setEIDFn setEndpointIDFn, getEIDFn getEndpointIDFn, edgeID string, containerPlatform agent.ContainerPlatform, metaFields agent.EdgeMetaFields, httpClient *edgeHTTPClient) *PortainerAsyncClient {
	initialCommandTimestamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return &PortainerAsyncClient{
		serverAddress:           serverAddress,
		setEndpointIDFn:         setEIDFn,
		getEndpointIDFn:         getEIDFn,
		edgeID:                  edgeID,
		httpClient:              httpClient,
		agentPlatformIdentifier: containerPlatform,
		commandTimestamp:        &initialCommandTimestamp,
		metaFields:              metaFields,
	}
}

func (client *PortainerAsyncClient) SetTimeout(t time.Duration) {
	client.httpClient.httpClient.Timeout = t
}

type MetaFields struct {
	EdgeGroupsIDs      []int `json:"edgeGroupsIds"`
	TagsIDs            []int `json:"tagsIds"`
	EnvironmentGroupID int   `json:"environmentGroupId"`
}

type AsyncRequest struct {
	CommandTimestamp *time.Time           `json:"commandTimestamp,omitempty"`
	Snapshot         *snapshot            `json:"snapshot,omitempty"`
	EndpointId       portainer.EndpointID `json:"endpointId,omitempty"`
	MetaFields       *MetaFields          `json:"metaFields"`
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
	Docker      *portainer.DockerSnapshot `json:"docker,omitempty"`
	DockerPatch jsondiff.Patch            `json:"dockerPatch,omitempty"`
	DockerHash  *uint32                   `json:"dockerHash,omitempty"`

	Kubernetes      *portainer.KubernetesSnapshot `json:"kubernetes,omitempty"`
	KubernetesPatch jsondiff.Patch                `json:"kubernetesPatch,omitempty"`
	KubernetesHash  *uint32                       `json:"kubernetesHash,omitempty"`

	StackLogs        []EdgeStackLog                                                  `json:"stackLogs,omitempty"`
	StackStatusArray map[portainer.EdgeStackID][]portainer.EdgeStackDeploymentStatus `json:"stackStatusArray,omitempty"`
	JobsStatus       map[portainer.EdgeJobID]agent.EdgeJobStatus                     `json:"jobsStatus,omitempty"`
	EdgeConfigStates map[EdgeConfigID]EdgeConfigStateType                            `json:"edgeConfigStates,omitempty"`
}

type AsyncResponse struct {
	PingInterval     time.Duration `json:"pingInterval"`
	SnapshotInterval time.Duration `json:"snapshotInterval"`
	CommandInterval  time.Duration `json:"commandInterval"`

	EndpointID       portainer.EndpointID `json:"endpointID"`
	Commands         []AsyncCommand       `json:"commands"`
	NeedFullSnapshot bool                 `json:"needFullSnapshot"`
}

type AsyncCommand struct {
	ID         int                  `json:"id"`
	Type       string               `json:"type"`
	EndpointID portainer.EndpointID `json:"endpointID"`
	Timestamp  time.Time            `json:"timestamp"`
	Operation  string               `json:"op"`
	Path       string               `json:"path"`
	Value      any                  `json:"value"`
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

type ContainerCommandData struct {
	ContainerName          string
	ContainerStartOptions  container.StartOptions
	ContainerRemoveOptions container.RemoveOptions
	ContainerOperation     string
}

type ImageCommandData struct {
	ImageName          string
	ImageRemoveOptions image.RemoveOptions
	ImageOperation     string
}

type VolumeCommandData struct {
	VolumeName      string
	ForceRemove     bool
	VolumeOperation string
}

type NormalStackCommandData struct {
	Name             string
	StackFileContent string
	StackOperation   string
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
				log.Warn().Err(err).Msg("could not create the Docker snapshot")
			}

			optimizeDockerSnapshot(dockerSnapshot)

			payload.Snapshot.Docker = dockerSnapshot
			currentSnapshot.Docker = dockerSnapshot

			if client.lastSnapshot.Docker != nil && !client.snapshotRetried {
				h, ok := snapshotHash(client.lastSnapshot.Docker)
				if ok {
					dockerPatch, err := jsondiff.Compare(client.lastSnapshot.Docker, dockerSnapshot)
					if err == nil {
						payload.Snapshot.DockerPatch = dockerPatch
						payload.Snapshot.DockerHash = &h
						payload.Snapshot.Docker = nil
					} else {
						log.Warn().Err(err).Msg("could not generate the Docker snapshot patch")
					}
				}
			}

			for _, stack := range client.stackLogCollectionQueue {
				cs, err := docker.GetContainersWithLabel("com.docker.compose.project=edge_" + stack.EdgeStackName)
				if err != nil {
					log.Warn().
						Str("stack", stack.EdgeStackName).
						Err(err).
						Msg("could not retrieve containers for stack")

					continue
				}

				cs2, err := docker.GetContainersWithLabel("com.docker.stack.namespace=edge_" + stack.EdgeStackName)
				if err != nil {
					log.Warn().Err(err).Msg("could not retrieve containers for stack")

					continue
				}

				cs = append(cs, cs2...)

				edgeStackLog := EdgeStackLog{
					EdgeStackID: stack.EdgeStackID,
				}

				for _, c := range cs {
					stdOut, stdErr, err := docker.GetContainerLogs(c.ID, strconv.Itoa(stack.Tail))
					if err != nil {
						log.Warn().
							Str("container_id", c.ID).
							Err(err).
							Msg("could not retrieve logs for container")

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
				log.Warn().Err(err).Msg("could not create the Kubernetes snapshot")
			}

			payload.Snapshot.Kubernetes = kubeSnapshot
			currentSnapshot.Kubernetes = kubeSnapshot

			if client.lastSnapshot.Kubernetes != nil && !client.snapshotRetried {
				h, ok := snapshotHash(client.lastSnapshot.Kubernetes)
				if ok {
					kubePatch, err := jsondiff.Compare(client.lastSnapshot.Docker, kubeSnapshot)
					if err == nil {
						payload.Snapshot.KubernetesPatch = kubePatch
						payload.Snapshot.KubernetesHash = &h
						payload.Snapshot.KubernetesPatch = nil
					} else {
						log.Warn().Err(err).Msg("could not generate the Kubernetes snapshot patch")
					}
				}
			}
		}

		client.nextSnapshotMutex.Lock()
		payload.Snapshot.StackStatusArray = client.nextSnapshot.StackStatusArray
		payload.Snapshot.JobsStatus = client.nextSnapshot.JobsStatus
		payload.Snapshot.EdgeConfigStates = client.nextSnapshot.EdgeConfigStates
		client.nextSnapshotMutex.Unlock()
	}

	if doCommand {
		payload.CommandTimestamp = client.commandTimestamp
	}

	if len(client.metaFields.EdgeGroupsIDs) > 0 || len(client.metaFields.TagsIDs) > 0 || client.metaFields.EnvironmentGroupID > 0 {
		payload.MetaFields = &MetaFields{
			EdgeGroupsIDs:      client.metaFields.EdgeGroupsIDs,
			TagsIDs:            client.metaFields.TagsIDs,
			EnvironmentGroupID: client.metaFields.EnvironmentGroupID,
		}
	}

	asyncResponse, err := client.executeAsyncRequest(payload, pollURL)
	if err != nil {
		return nil, err
	}

	if doSnapshot && asyncResponse.NeedFullSnapshot && !client.snapshotRetried {
		log.Debug().Msg("retrying with full snapshot")
		client.snapshotRetried = true

		_, err = client.GetEnvironmentStatus("snapshot")
		if err != nil {
			log.Error().Err(err).Msg("unable to resend the full snapshot")
		}
	} else if doSnapshot {
		client.snapshotRetried = false

		client.lastSnapshot.Docker = currentSnapshot.Docker
		client.lastSnapshot.Kubernetes = currentSnapshot.Kubernetes

		if client.lastSnapshot.StackStatusArray == nil {
			client.lastSnapshot.StackStatusArray = make(map[portainer.EdgeStackID][]portainer.EdgeStackDeploymentStatus)
		}

		for k, v := range client.nextSnapshot.StackStatusArray {
			client.lastSnapshot.StackStatusArray[k] = v
		}

		client.nextSnapshot.StackStatusArray = nil
		client.nextSnapshot.JobsStatus = nil
		client.nextSnapshot.EdgeConfigStates = nil
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
	req.Header.Set(agent.HTTPResponseAgentHeaderName, agent.Version)
	req.Header.Set(agent.HTTPResponseAgentTimeZone, time.Local.String())
	req.Header.Set(agent.HTTPResponseUpdateIDHeaderName, strconv.Itoa(client.metaFields.UpdateID))
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatformIdentifier)))

	log.Debug().
		Str(agent.HTTPEdgeIdentifierHeaderName, client.edgeID).
		Int(agent.HTTPResponseUpdateIDHeaderName, (client.metaFields.UpdateID)).
		Int(agent.HTTPResponseAgentPlatform, (int(client.agentPlatformIdentifier))).
		Str(agent.HTTPResponseAgentHeaderName, agent.Version).
		Str(agent.HTTPResponseAgentTimeZone, time.Local.String()).
		Msg("sending async request with headers")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorData := parseError(resp)
		logError(resp, errorData)

		if errorData != nil {
			return nil, errors.New(errorData.Message + ": " + errorData.Details)
		}

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
func (client *PortainerAsyncClient) SetEdgeStackStatus(edgeStackID int, edgeStackStatus portainer.EdgeStackStatusType, rollbackTo *int, err string) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if client.nextSnapshot.StackStatusArray == nil {
		client.nextSnapshot.StackStatusArray = make(map[portainer.EdgeStackID][]portainer.EdgeStackDeploymentStatus)
	}

	status, ok := client.nextSnapshot.StackStatusArray[portainer.EdgeStackID(edgeStackID)]
	if !ok {
		status = []portainer.EdgeStackDeploymentStatus{}
	}

	if edgeStackStatus == portainer.EdgeStackStatusRemoved {
		status = []portainer.EdgeStackDeploymentStatus{}
	} else {
		status = append(status, portainer.EdgeStackDeploymentStatus{
			Type:       edgeStackStatus,
			Error:      err,
			RollbackTo: rollbackTo,
			Time:       time.Now().Unix(),
		})
	}

	client.nextSnapshot.StackStatusArray[portainer.EdgeStackID(edgeStackID)] = status

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
func (client *PortainerAsyncClient) GetEdgeStackConfig(edgeStackID int, version *int) (*edge.StackPayload, error) {
	// Async mode MUST NOT make any extra requests to Portainer, all the
	// information exchange needs to happen via the async polling loop, which
	// uses /endpoints/edge/async. This is a strict requirement.
	return nil, nil // unused in async mode
}

func (client *PortainerAsyncClient) GetEdgeConfig(id EdgeConfigID) (*EdgeConfig, error) {
	return nil, nil // unused in async mode
}

func (client *PortainerAsyncClient) SetEdgeConfigState(id EdgeConfigID, state EdgeConfigStateType) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if client.nextSnapshot.EdgeConfigStates == nil {
		client.nextSnapshot.EdgeConfigStates = make(map[EdgeConfigID]EdgeConfigStateType)
	}

	client.nextSnapshot.EdgeConfigStates[id] = state

	return nil
}

func (client *PortainerAsyncClient) EnqueueLogCollectionForStack(logCmd LogCommandData) {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	client.stackLogCollectionQueue = append(client.stackLogCollectionQueue, logCmd)
}

func snapshotHash(snapshot any) (uint32, bool) {
	b := &bytes.Buffer{}

	err := json.NewEncoder(b).Encode(snapshot)
	if err != nil {
		log.Error().Err(err).Msg("could not encode the snapshot")

		return 0, false
	}

	h := fnv.New32a()
	h.Write(bytes.TrimSpace(b.Bytes()))

	return h.Sum32(), true
}

func optimizeDockerSnapshot(s *portainer.DockerSnapshot) {
	sort.Slice(s.SnapshotRaw.Networks, func(i, j int) bool {
		return s.SnapshotRaw.Networks[i].Name < s.SnapshotRaw.Networks[j].Name
	})

	sort.Slice(s.SnapshotRaw.Volumes.Volumes, func(i, j int) bool {
		return s.SnapshotRaw.Volumes.Volumes[i].Name < s.SnapshotRaw.Volumes.Volumes[j].Name
	})

	for k := range s.SnapshotRaw.Containers {
		sort.Slice(s.SnapshotRaw.Containers[k].Mounts, func(i, j int) bool {
			return s.SnapshotRaw.Containers[k].Mounts[i].Name < s.SnapshotRaw.Containers[k].Mounts[j].Name
		})
	}
}
