package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/kubernetes"
	portainer "github.com/portainer/portainer/api"
)

// PortainerAsyncClient is used to execute HTTP requests using only the /api/entrypoint/async api endpoint
type PortainerAsyncClient struct {
	httpClient              *http.Client
	serverAddress           string
	endpointID              string
	edgeID                  string
	agentPlatformIdentifier agent.ContainerPlatform
}

// NewPortainerAsyncClient returns a pointer to a new PortainerAsyncClient instance
func NewPortainerAsyncClient(serverAddress, endpointID, edgeID string, containerPlatform agent.ContainerPlatform, httpClient *http.Client) *PortainerAsyncClient {
	return &PortainerAsyncClient{
		serverAddress:           serverAddress,
		endpointID:              endpointID,
		edgeID:                  edgeID,
		httpClient:              httpClient,
		agentPlatformIdentifier: containerPlatform,
	}
}

func (client *PortainerAsyncClient) SetTimeout(t time.Duration) {
	client.httpClient.Timeout = t
}

// TODO: figure out where this should be stored - or better yet, rewrite as a command/exec/diff channel that the execution system is listening to.
var lastAsyncResponse AsyncResponse
var lastAsyncResponseMutex sync.Mutex

var nextSnapshotRequest AsyncRequest
var nextSnapshotMutex sync.Mutex

//TODO: copied from portainer/api/http/handler/endpointedge/async.go
type EdgeId int
type Snapshot struct {
	Docker     *portainer.DockerSnapshot
	Kubernetes *portainer.KubernetesSnapshot
}
type AsyncRequest struct {
	CommandId   string                               `json: optional` // TODO: need to figure out a safe value to store - server timestamp might work - though that would require stacks&jobs to have a last modified timestamp
	Snapshot    Snapshot                             `json: optional` // todo
	StackStatus map[EdgeId]setEdgeStackStatusPayload `json: optional` // TODO: this should be in the snapshot interval... (probably)
}
type edgeStackData struct {
	ID               int
	Version          int
	StackFileContent string
	Name             string
}

type JSONPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value"`
}
type AsyncResponse struct {
	CommandInterval  time.Duration `json: optional` //TODO: these should determine when and what payloads are filled
	PingInterval     time.Duration `json: optional`
	SnapshotInterval time.Duration `json: optional`

	ServerCommandId      string      // should be easy to detect if its larger / smaller:  this is the response that tells the agent there are new commands waiting for it
	SendDiffSnapshotTime time.Time   `json: optional` // might be optional
	Commands             []JSONPatch `json: optional` // todo
	Status               string      // give the agent some idea if the server thinks its OK, or if it should STOP
}

// TODO: how to make "command list be only the things that have versions newer than the commandid.."

func (client *PortainerAsyncClient) GetEnvironmentStatus() (*PollStatusResponse, error) {
	pollURL := fmt.Sprintf("%s/api/endpoints/edge/async/", client.serverAddress)

	payload := AsyncRequest{
		CommandId: "9999", // TODO: this should only be set for the CommandInterval
	}

	// TODO: some of this this should be optional - SnapshotInterval
	nextSnapshotMutex.Lock()
	defer nextSnapshotMutex.Unlock()
	if nextSnapshotRequest.StackStatus != nil {
		payload.StackStatus = nextSnapshotRequest.StackStatus
		nextSnapshotRequest.StackStatus = nil
	}
	switch client.agentPlatformIdentifier {
	// TODO: this switch statement is a hint that there should really be a "platform" interface that all platforms have - which would implement a "CreateSnapshot()" - tho that would change how the datamodel works too
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
	// end Snapshot

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", pollURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)
	req.Header.Set(agent.HTTPAgentVersionHeaderName, agent.Version)
	req.Header.Set(agent.HTTPAgentPIDName, fmt.Sprintf("%d", os.Getpid()))
	//req.Header.Set(agent.HTTPAgentUUIDHeaderName, agent.getUUID())	// TODO: this needs to be unique to the orchestrator deployment - so its possible to disceren the difference between multiple orchestrators on the same system

	// TODO: should this be set for all requests? (and should all the common code be extracted...)
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatformIdentifier)))

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

	if strings.HasPrefix(asyncResponse.Status, "STOP") {
		// TODO: if we're a container, we need to actually stop the container, otherwise, exiting will cause Docker to re-start
		// testing with:
		// docker run --name agent-2.11.3 -dit --restart always -v /var/run:/var/run -v /home/sven:/home/sven -v $(pwd)/dist:/app/ --workdir $(pwd) ubuntu bash -c "pwd && ls -al ./dist && ./dist/agent-2.11.3 --EdgeMode --EdgeAsyncMode --EdgeKey aHR0cHM6Ly9wb3J0YWluZXIucDEuYWxoby5zdDo5NDQzfHBvcnRhaW5lci5wMS5hbGhvLnN0OjgwMDB8Yzk6NWM6NmM6ZGI6MjM6ODk6ZjQ6NWY6NDQ6YjE6ZmE6MjM6NGI6MTM6MTM6MGR8Mw --EdgeID 7e2b0143-c511-43c3-844c-a7a91ab0bedc --LogLevel DEBUG --sslcert /home/sven/.config/portainer/certs/agent-cert.pem --sslkey /home/sven/.config/portainer/certs/agent-key.pem --sslcacert /home/sven/.config/portainer/certs/ca.pem"
		// Should really share /tmp /data etc for a real test...
		// TODO: consider telling the agent what other container there is, so it can introspect?
		log.Printf("Portainer server has asked us to %s - likely because there are 2 agents talking to it for this endpoint", asyncResponse.Status)
		agent.StopThisAgent(asyncResponse.Status)

		return &PollStatusResponse{
			Status:          asyncResponse.Status,
			CheckinInterval: asyncResponse.PingInterval.Seconds(),
		}, nil
	}
	log.Printf("[DEBUG] [internal,edge,poll] [message: Portainer agent status: %s]", asyncResponse.Status)

	// TODO: Store the other parts of the response to use for the other "requests"
	lastAsyncResponseMutex.Lock()
	lastAsyncResponse = asyncResponse
	lastAsyncResponseMutex.Unlock()

	var stacks []StackStatus
	var schedules []agent.Schedule
	for _, command := range asyncResponse.Commands {
		if strings.HasPrefix(command.Path, "/edgestack/") {
			// assume op:add
			var stack edgeStackData
			err := mapstructure.Decode(command.Value, &stack)
			if err != nil {
				log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to edgeStackData", command.Value)
				continue
			}
			stacks = append(stacks, StackStatus{
				ID:      stack.ID,
				Version: stack.Version,
			})
			log.Printf("[DEBUG] [http,client,portainer] GetEnvironmentStatus stack %d, version: %d", stack.ID, stack.Version)
		} else if strings.HasPrefix(command.Path, "/edgejob/") {
			// assume op:add
			var job agent.Schedule
			err := mapstructure.Decode(command.Value, &job)
			if err != nil {
				log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to agent.Schedule", command.Value)
				continue
			}
			schedules = append(schedules, agent.Schedule{
				ID:             job.ID,
				Version:        job.Version,
				CronExpression: job.CronExpression,
				Script:         job.Script,
				CollectLogs:    job.CollectLogs,
			})
			log.Printf("[DEBUG] [http,client,portainer] GetEnvironmentStatus job %d, version: %d", job.ID, job.Version)
		}

	}

	return &PollStatusResponse{
		Status:          "NOTUNNEL",
		CheckinInterval: asyncResponse.PingInterval.Seconds(),
		Stacks:          stacks,
		Schedules:       schedules,
	}, nil
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
// TODO: this should retreive the config from the internal to agent structure, not the ephemeral "lastAsyncResponse"
// likely works ok while we're using add only..
func (client *PortainerAsyncClient) GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error) {
	lastAsyncResponseMutex.Lock()
	defer lastAsyncResponseMutex.Unlock()

	var config agent.EdgeStackConfig
	for _, command := range lastAsyncResponse.Commands {
		if strings.HasPrefix(command.Path, "/edgestack/") {
			// assume op:add
			var stack edgeStackData
			err := mapstructure.Decode(command.Value, &stack)
			if err != nil {
				log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to edgeStackData", command.Value)
				continue
			}
			if stack.ID == edgeStackID {
				config = agent.EdgeStackConfig{
					Name:        stack.Name,
					FileContent: stack.StackFileContent,
					//ImageMapping: ,
				}
				log.Printf("[DEBUG] [http,client,portainer] GetEdgeStackConfig %s", config.Name)

				return &config, nil
			}
		}

	}

	log.Printf("[ERROR] [http,client,portainer] GetEdgeStackConfig(%d) not found", edgeStackID)

	return nil, fmt.Errorf("GetEdgeStackConfig(%d) not found", edgeStackID)
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerAsyncClient) SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error {
	// This should go into the next snapshot payload
	endpointID, err := strconv.Atoi(client.endpointID)
	if err != nil {
		return err
	}
	nextSnapshotMutex.Lock()
	defer nextSnapshotMutex.Unlock()
	if nextSnapshotRequest.StackStatus == nil {
		nextSnapshotRequest.StackStatus = make(map[EdgeId]setEdgeStackStatusPayload)
	}
	nextSnapshotRequest.StackStatus[EdgeId(edgeStackID)] = setEdgeStackStatusPayload{
		Error:      error,
		Status:     edgeStackStatus,
		EndpointID: endpointID,
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
