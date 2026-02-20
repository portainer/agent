package client

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"hash/fnv"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/kubernetes"
	aos "github.com/portainer/agent/os"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/klauspost/compress/gzhttp/writer/gzkp"
	"github.com/klauspost/compress/gzip"
	"github.com/rs/zerolog/log"
	"github.com/wI2L/jsondiff"
)

type versionAndTS struct {
	version   int
	timestamp time.Time
}

// PortainerAsyncClient is used to execute HTTP requests using only the /api/entrypoint/async api endpoint
type PortainerAsyncClient struct {
	version string

	httpClient              *edgeHTTPClient
	serverAddress           string
	setEndpointIDFn         setEndpointIDFn
	getEndpointIDFn         getEndpointIDFn
	edgeID                  string
	edgeKey                 string
	agentPlatformIdentifier agent.ContainerPlatform
	commandTimestamp        time.Time
	timeZoneStr             string // Cached timezone string to avoid repeated allocations
	pendingESCommandsTS     map[portainer.EdgeStackID]versionAndTS
	pendingCmdsMutex        sync.Mutex
	metaFields              agent.EdgeMetaFields

	lastAsyncResponse AsyncResponse
	lastSnapshot      snapshot
	nextSnapshot      snapshot
	nextSnapshotMutex sync.Mutex
	snapshotRetried   bool

	stackLogCollectionQueue []LogCommandData
	liveLogCollectors       map[string]*LiveLogCollector

	policyHelmCharts *PolicyHelmCharts

	createSnapshotFn DockerSnapshotter
}

// NewPortainerAsyncClient returns a pointer to a new PortainerAsyncClient instance
func NewPortainerAsyncClient(
	serverAddress string,
	setEIDFn setEndpointIDFn,
	getEIDFn getEndpointIDFn,
	edgeID string,
	edgeKey string,
	containerPlatform agent.ContainerPlatform,
	metaFields agent.EdgeMetaFields,
	httpClient *edgeHTTPClient,
	opts ...Option,
) *PortainerAsyncClient {
	clientOpts := defaultOptions()
	for _, o := range opts {
		o(clientOpts)
	}

	return &PortainerAsyncClient{
		version:                 clientOpts.version,
		serverAddress:           serverAddress,
		setEndpointIDFn:         setEIDFn,
		getEndpointIDFn:         getEIDFn,
		edgeID:                  edgeID,
		edgeKey:                 edgeKey,
		httpClient:              httpClient,
		agentPlatformIdentifier: containerPlatform,
		commandTimestamp:        time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		timeZoneStr:             time.Local.String(),
		pendingESCommandsTS:     make(map[portainer.EdgeStackID]versionAndTS),
		metaFields:              metaFields,
		createSnapshotFn:        clientOpts.dockerSnapshotter,
		liveLogCollectors:       make(map[string]*LiveLogCollector),
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
	MetaFields       *MetaFields          `json:"metaFields,omitempty"`
}

type EndpointLog struct {
	DockerContainerID string `json:"dockerContainerID,omitempty"`
	StdOut            string `json:"stdOut,omitempty"`
	StdErr            string `json:"stdErr,omitempty"`
}

type EdgeStackLog struct {
	EdgeStackID portainer.EdgeStackID `json:"edgeStackID,omitempty"`
	Logs        []EndpointLog         `json:"logs,omitempty"`
	Append      bool                  `json:"append,omitempty"`
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
	PolicyStatus     map[string][]portainer.PolicyChartStatus                        `json:"policyStatus"`
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
	ContainerID   string
	Tail          int
	Since         string
	Until         string
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
	RemoveVolumes    bool
}

type DockerSnapshotter func(edgeKey string) (*portainer.DockerSnapshot, error)

func (client *PortainerAsyncClient) GetEnvironmentID() (portainer.EndpointID, error) {
	return 0, errors.New("GetEnvironmentID is not available in async mode")
}

func (client *PortainerAsyncClient) GetEnvironmentStatus(flags ...string) (*PollStatusResponse, error) {
	pollURL := client.serverAddress + "/api/endpoints/edge/async"

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
			client.createDockerSnapshot(&payload, &currentSnapshot)

		case agent.PlatformKubernetes:
			client.createKubernetesSnapshot(&payload, &currentSnapshot)
		}

		client.nextSnapshotMutex.Lock()
		payload.Snapshot.StackStatusArray = client.nextSnapshot.StackStatusArray
		payload.Snapshot.JobsStatus = client.nextSnapshot.JobsStatus
		payload.Snapshot.EdgeConfigStates = client.nextSnapshot.EdgeConfigStates
		payload.Snapshot.PolicyStatus = client.nextSnapshot.PolicyStatus
		client.nextSnapshotMutex.Unlock()
	}

	if doCommand {
		minTS := client.commandTimestamp

		// Send a timestamp slightly smaller than the earliest pending command
		// timestamp to ensure it gets re-sent if the Agent crashes before
		// fully processing it
		client.pendingCmdsMutex.Lock()
		for _, ts := range client.pendingESCommandsTS {
			if !ts.timestamp.After(minTS) {
				minTS = ts.timestamp.Add(-time.Microsecond)
			}
		}
		client.pendingCmdsMutex.Unlock()

		payload.CommandTimestamp = &minTS
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

	if doSnapshot {
		client.rotateSnapshots(currentSnapshot, asyncResponse)
	}

	client.setEndpointIDFn(asyncResponse.EndpointID)

	// Filter out already received commands
	asyncResponse.Commands = slices.DeleteFunc(asyncResponse.Commands, func(e AsyncCommand) bool {
		return !e.Timestamp.After(client.commandTimestamp)
	})

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

	gz := gzkp.NewWriter(buf, gzip.BestCompression)

	if _, err := gz.Write(data); err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	if len(data) < buf.Len() {
		return nil, errors.New("compressed data is larger than original data")
	}

	return buf, nil
}

func (client *PortainerAsyncClient) executeAsyncRequest(payload AsyncRequest, pollURL string) (*AsyncResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	gzipped := false
	buf := bytes.NewBuffer(data)

	if payload.Snapshot != nil {
		if gzBuf, err := gzipCompress(data); err == nil {
			buf = gzBuf
			gzipped = true
		}
	}

	req, err := http.NewRequest("POST", pollURL, buf)
	if err != nil {
		return nil, err
	}

	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)
	req.Header.Set(agent.HTTPResponseAgentHeaderName, client.version)
	req.Header.Set(agent.HTTPResponseAgentTimeZone, client.timeZoneStr)
	req.Header.Set(agent.HTTPResponseUpdateIDHeaderName, strconv.Itoa(client.metaFields.UpdateID))
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatformIdentifier)))

	// Send container engine info only during initialization (endpointID == 0)
	if client.getEndpointIDFn() == 0 {
		osModule := aos.DetermineContainerPlatform()
		if containerEngine := aos.GetContainerEngineName(osModule); containerEngine != "" {
			req.Header.Set(agent.HTTPResponseAgentContainerEngine, containerEngine)
		}
	}

	log.Debug().
		Str(agent.HTTPEdgeIdentifierHeaderName, client.edgeID).
		Int(agent.HTTPResponseUpdateIDHeaderName, client.metaFields.UpdateID).
		Int(agent.HTTPResponseAgentPlatform, int(client.agentPlatformIdentifier)).
		Str(agent.HTTPResponseAgentHeaderName, client.version).
		Str(agent.HTTPResponseAgentTimeZone, time.Local.String()).
		Int("endpoint_id", int(client.getEndpointIDFn())).
		Msg("sending async request with headers")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		errorData := parseError(resp)
		logError(resp, errorData)

		if errorData != nil {
			return nil, newNonOkResponseError(errorData.Message + ": " + errorData.Details)
		}

		return nil, newNonOkResponseError("short poll request failed")
	}

	var asyncResponse AsyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&asyncResponse); err != nil {
		return nil, err
	}

	return &asyncResponse, nil
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerAsyncClient) SetEdgeStackStatus(edgeStackID, version int, edgeStackStatus portainer.EdgeStackStatusType, rollbackTo *int, err string) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if client.nextSnapshot.StackStatusArray == nil {
		client.nextSnapshot.StackStatusArray = make(map[portainer.EdgeStackID][]portainer.EdgeStackDeploymentStatus)
	}

	status, ok := client.nextSnapshot.StackStatusArray[portainer.EdgeStackID(edgeStackID)]
	if !ok {
		status = []portainer.EdgeStackDeploymentStatus{}
	}

	switch edgeStackStatus {
	case portainer.EdgeStackStatusRunning, portainer.EdgeStackStatusRemoved, portainer.EdgeStackStatusCompleted, portainer.EdgeStackStatusError:
		client.pendingCmdsMutex.Lock()
		if pc, ok := client.pendingESCommandsTS[portainer.EdgeStackID(edgeStackID)]; ok && pc.version == version {
			delete(client.pendingESCommandsTS, portainer.EdgeStackID(edgeStackID))
		}
		client.pendingCmdsMutex.Unlock()
	}

	if edgeStackStatus == portainer.EdgeStackStatusRemoved {
		status = []portainer.EdgeStackDeploymentStatus{}
	} else {
		status = append(status, portainer.EdgeStackDeploymentStatus{
			Type:       edgeStackStatus,
			Error:      err,
			RollbackTo: rollbackTo,
			Time:       time.Now().Unix(),
			Version:    version,
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
	client.commandTimestamp = timestamp
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

func (client *PortainerAsyncClient) SetChartsResponse(chart *PolicyHelmCharts) {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	client.policyHelmCharts = chart
}

// GetCharts retrieves the chart contents for the specified charts from the Portainer server
func (client *PortainerAsyncClient) GetCharts(chartNames []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if client.policyHelmCharts == nil {
		return nil, nil, errors.New("BUG: policyHelmCharts is nil, this should never happen")
	}

	return client.policyHelmCharts.PolicyChartBundles, client.policyHelmCharts.RestoreSettingsBundle, nil
}

func (client *PortainerAsyncClient) UpdatePolicyChartStatuses(statuses []portainer.PolicyChartStatus) error {
	client.nextSnapshotMutex.Lock()
	defer client.nextSnapshotMutex.Unlock()

	if len(statuses) == 0 {
		return nil
	}

	if client.nextSnapshot.PolicyStatus == nil {
		client.nextSnapshot.PolicyStatus = make(map[string][]portainer.PolicyChartStatus)
	}

	for _, status := range statuses {
		client.nextSnapshot.PolicyStatus[status.ChartName] = append(client.nextSnapshot.PolicyStatus[status.ChartName], status)
	}

	return nil
}

// SetPendingCommand stores the latest command timestamp for a given stack ID
func (client *PortainerAsyncClient) SetPendingCommand(id portainer.EdgeStackID, version int, timestamp time.Time) {
	client.pendingCmdsMutex.Lock()
	defer client.pendingCmdsMutex.Unlock()

	client.pendingESCommandsTS[id] = versionAndTS{version: version, timestamp: timestamp}
}

func (client *PortainerAsyncClient) createDockerSnapshot(payload *AsyncRequest, currentSnapshot *snapshot) {
	dockerSnapshot, err := client.createSnapshotFn(client.edgeKey)
	if err != nil {
		log.Warn().Err(err).Msg("could not create the Docker snapshot")

		return
	}

	payload.Snapshot.Docker = dockerSnapshot
	currentSnapshot.Docker = dockerSnapshot

	client.getEdgeStackLogs(payload)

	if client.lastSnapshot.Docker == nil || client.snapshotRetried {
		return
	}

	dockerPatch, err := jsondiff.Compare(client.lastSnapshot.Docker, dockerSnapshot)
	if err != nil {
		log.Warn().Err(err).Msg("could not generate the Docker snapshot patch")

		return
	}

	if isDockerSnapshotDiffEmpty(dockerPatch) {
		payload.Snapshot.Docker = nil

		return
	}

	h, ok := snapshotHash(client.lastSnapshot.Docker)
	if !ok {
		return
	}

	currentSnapshot.Docker = nil

	payload.Snapshot.DockerPatch = dockerPatch
	payload.Snapshot.DockerHash = &h
	payload.Snapshot.Docker = nil
}

var noisyFieldRegexps = []*regexp.Regexp{
	regexp.MustCompile("^/DockerSnapshotRaw/Containers/[0-9]+/Status$"),
}

// isDockerSnapshotDiffEmpty filters out diffs that only contain fields that
// change all the time, so the server needs to process less of them
func isDockerSnapshotDiffEmpty(dockerPatch jsondiff.Patch) bool {
	noisyFields := []string{
		"/DiagnosticsData/DNS/edge-to-portainer",
		"/DiagnosticsData/Telnet/edge-to-portainer",
		"/DockerSnapshotRaw/Info/NFd",
		"/DockerSnapshotRaw/Info/NGoroutines",
		"/DockerSnapshotRaw/Info/SystemTime",
		"/Time",
	}

outerLoop:
	for _, op := range dockerPatch {
		// Skip noisy field regexps
		for _, noisyRegexp := range noisyFieldRegexps {
			if noisyRegexp.MatchString(op.Path) {
				continue outerLoop
			}
		}

		if op.Type != "replace" || !slices.Contains(noisyFields, op.Path) {
			return false
		}
	}

	return true
}

func (client *PortainerAsyncClient) createKubernetesSnapshot(payload *AsyncRequest, currentSnapshot *snapshot) {
	kubeSnapshot, err := kubernetes.CreateSnapshot(client.edgeKey)
	if err != nil {
		log.Warn().Err(err).Msg("could not create the Kubernetes snapshot")

		return
	}

	payload.Snapshot.Kubernetes = kubeSnapshot
	currentSnapshot.Kubernetes = kubeSnapshot

	if client.lastSnapshot.Kubernetes == nil || client.snapshotRetried {
		return
	}

	h, ok := snapshotHash(client.lastSnapshot.Kubernetes)
	if !ok {
		return
	}

	kubePatch, err := jsondiff.Compare(client.lastSnapshot.Kubernetes, kubeSnapshot)
	if err != nil {
		log.Warn().Err(err).Msg("could not generate the Kubernetes snapshot patch")

		return
	}

	payload.Snapshot.KubernetesPatch = kubePatch
	payload.Snapshot.KubernetesHash = &h
	payload.Snapshot.KubernetesPatch = nil
}

func (client *PortainerAsyncClient) getEdgeStackLogs(payload *AsyncRequest) {
	// Running live log collection
	for containerID, logCollector := range client.liveLogCollectors {
		client.collectAvailableLogs(payload, logCollector, containerID, false)
	}

	// New log collection
	for _, logCmd := range client.stackLogCollectionQueue {
		var csIDs []string

		// Whole edge stack
		if logCmd.ContainerID == "" {
			cs := getContainersFromEdgeStack(logCmd.EdgeStackName)

			for _, c := range cs {
				csIDs = append(csIDs, c.ID)
			}
		} else { // Just one container
			client.startLiveLogCollection(payload, logCmd.ContainerID, logCmd.Since, logCmd.Until, strconv.Itoa(logCmd.Tail))

			continue
		}

		edgeStackLog := EdgeStackLog{EdgeStackID: logCmd.EdgeStackID}

		for _, cID := range csIDs {
			stdOut, stdErr, err := docker.GetContainerLogs(cID, strconv.Itoa(logCmd.Tail), logCmd.Since, logCmd.Until)
			if err != nil {
				log.Warn().
					Err(err).
					Str("container_id", cID).
					Msg("could not retrieve logs for container")

				continue
			}

			edgeStackLog.Logs = append(edgeStackLog.Logs, EndpointLog{
				DockerContainerID: cID,
				StdOut:            string(stdOut),
				StdErr:            string(stdErr),
			})
		}

		if len(edgeStackLog.Logs) > 0 {
			payload.Snapshot.StackLogs = append(payload.Snapshot.StackLogs, edgeStackLog)
		}
	}
}

func (client *PortainerAsyncClient) collectAvailableLogs(payload *AsyncRequest, logCollector *LiveLogCollector, containerID string, firstTime bool) {
	stdOut, stdErr, done := logCollector.Collect()

	if done {
		log.Debug().Str("container_id", containerID).Msg("removing live log collector")

		delete(client.liveLogCollectors, containerID)
	}

	payload.Snapshot.StackLogs = append(payload.Snapshot.StackLogs, EdgeStackLog{
		Logs: []EndpointLog{
			{
				DockerContainerID: containerID,
				StdOut:            string(stdOut),
				StdErr:            string(stdErr),
			},
		},
		Append: !firstTime,
	})
}

func (client *PortainerAsyncClient) startLiveLogCollection(payload *AsyncRequest, containerID, since, until, tail string) {
	log.Debug().
		Str("container_id", containerID).
		Str("since", since).
		Str("until", until).
		Str("tail", tail).
		Msg("starting new live log collector")

	logCollector, err := StartNewLiveLogCollector(containerID, since, until, tail)
	if err != nil {
		log.Warn().Err(err).Str("container_id", containerID).Msg("could not retrieve logs for container")

		return
	}

	client.collectAvailableLogs(payload, logCollector, containerID, true)

	client.liveLogCollectors[containerID] = logCollector
}

func (client *PortainerAsyncClient) rotateSnapshots(currentSnapshot snapshot, asyncResponse *AsyncResponse) {
	if asyncResponse.NeedFullSnapshot && !client.snapshotRetried {
		log.Debug().Msg("retrying with full snapshot")

		client.snapshotRetried = true

		if _, err := client.GetEnvironmentStatus("snapshot"); err != nil {
			log.Error().Err(err).Msg("unable to resend the full snapshot")
		}

		return
	}

	client.snapshotRetried = false

	client.lastSnapshot.Docker = cmp.Or(currentSnapshot.Docker, client.lastSnapshot.Docker)
	client.lastSnapshot.Kubernetes = currentSnapshot.Kubernetes

	if client.lastSnapshot.StackStatusArray == nil {
		client.lastSnapshot.StackStatusArray = make(map[portainer.EdgeStackID][]portainer.EdgeStackDeploymentStatus)
	}

	maps.Copy(client.lastSnapshot.StackStatusArray, client.nextSnapshot.StackStatusArray)

	client.nextSnapshot.StackStatusArray = nil
	client.nextSnapshot.JobsStatus = nil
	client.nextSnapshot.EdgeConfigStates = nil
	client.nextSnapshot.PolicyStatus = nil
	client.stackLogCollectionQueue = nil
}

func snapshotHash(snapshot any) (uint32, bool) {
	b := &bytes.Buffer{}

	if err := json.NewEncoder(b).Encode(snapshot); err != nil {
		log.Error().Err(err).Msg("could not encode the snapshot")

		return 0, false
	}

	h := fnv.New32a()
	if _, err := h.Write(bytes.TrimSpace(b.Bytes())); err != nil {
		log.Error().Err(err).Msg("could not hash the snapshot")

		return 0, false
	}

	return h.Sum32(), true
}

func getContainersFromEdgeStack(edgeStackName string) []types.Container {
	cs, err := docker.GetContainersWithLabel("com.docker.compose.project=edge_" + edgeStackName)
	if err != nil {
		log.Warn().Err(err).Str("stack", edgeStackName).Msg("could not retrieve containers for stack")
	}

	cs2, err := docker.GetContainersWithLabel("com.docker.stack.namespace=edge_" + edgeStackName)
	if err != nil {
		log.Warn().Err(err).Msg("could not retrieve containers for stack")
	}

	return append(cs, cs2...)
}
