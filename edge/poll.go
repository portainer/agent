package edge

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockercli "github.com/docker/docker/client"
	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	"github.com/portainer/libcrypto"
)

const (
	clientDefaultPollTimeout    = 5
	tunnelActivityCheckInterval = 30 * time.Second
)

// PollService is used to poll a Portainer instance to retrieve the status associated to the Edge endpoint.
// It is responsible for:
// * managing the state of the reverse tunnel (open and closing after inactivity)
// * retrieving the data associated to Edge stacks and schedules
// * managing the agent auto update process
type PollService struct {
	apiServerAddr            string
	pollIntervalInSeconds    float64
	pollTicker               *time.Ticker
	inactivityTimeout        time.Duration
	edgeID                   string
	portainerClient          client.PortainerClient
	tunnelClient             agent.ReverseTunnelClient
	scheduleManager          agent.Scheduler
	lastActivity             time.Time
	updateLastActivitySignal chan struct{}
	startSignal              chan struct{}
	stopSignal               chan struct{}
	edgeStackManager         *stack.StackManager
	portainerURL             string
	endpointID               string
	tunnelServerAddr         string
	tunnelServerFingerprint  string
	logsManager              *scheduler.LogsManager
	// TODO: REVIEW
	// Hack for POC purposes only - to avoid triggering the update process on each poll
	// This should be replaced by a proper scheduling of the agent update
	autoUpdateTriggered bool
}

type pollServiceConfig struct {
	APIServerAddr           string
	EdgeID                  string
	InactivityTimeout       string
	PollFrequency           string
	TunnelCapability        bool
	PortainerURL            string
	EndpointID              string
	TunnelServerAddr        string
	TunnelServerFingerprint string
}

// newPollService returns a pointer to a new instance of PollService, and will start two loops in go routines.
// The first loop will poll the Portainer instance for the status of the associated endpoint and create a reverse tunnel
// if needed as well as manage schedules.
// The second loop will check for the last activity of the reverse tunnel and close the tunnel if it exceeds the tunnel
// inactivity duration.
// If TunnelCapability is disabled, it will only poll for Edge stacks and schedule without managing reverse tunnels.
func newPollService(edgeStackManager *stack.StackManager, logsManager *scheduler.LogsManager, config *pollServiceConfig, portainerClient client.PortainerClient) (*PollService, error) {
	pollFrequency, err := time.ParseDuration(config.PollFrequency)
	if err != nil {
		return nil, err
	}

	inactivityTimeout, err := time.ParseDuration(config.InactivityTimeout)
	if err != nil {
		return nil, err
	}

	pollService := &PollService{
		apiServerAddr:            config.APIServerAddr,
		edgeID:                   config.EdgeID,
		pollIntervalInSeconds:    pollFrequency.Seconds(),
		pollTicker:               time.NewTicker(pollFrequency),
		inactivityTimeout:        inactivityTimeout,
		scheduleManager:          scheduler.NewCronManager(),
		updateLastActivitySignal: make(chan struct{}),
		startSignal:              make(chan struct{}),
		stopSignal:               make(chan struct{}),
		edgeStackManager:         edgeStackManager,
		portainerURL:             config.PortainerURL,
		endpointID:               config.EndpointID,
		tunnelServerAddr:         config.TunnelServerAddr,
		tunnelServerFingerprint:  config.TunnelServerFingerprint,
		logsManager:              logsManager,
		portainerClient:          portainerClient,
		autoUpdateTriggered:      false,
	}

	if config.TunnelCapability {
		pollService.tunnelClient = chisel.NewClient()
	}

	go pollService.startStatusPollLoop()
	go pollService.startActivityMonitoringLoop()

	return pollService, nil
}

func (service *PollService) resetActivityTimer() {
	if service.tunnelClient != nil && service.tunnelClient.IsTunnelOpen() {
		service.updateLastActivitySignal <- struct{}{}
	}
}

func (service *PollService) Start() {
	service.startSignal <- struct{}{}
}

func (service *PollService) Stop() {
	service.stopSignal <- struct{}{}
}

func (service *PollService) startStatusPollLoop() {
	var pollCh <-chan time.Time

	log.Printf("[DEBUG] [edge] [poll_interval_seconds: %f] [server_url: %s] [message: starting Portainer short-polling client]", service.pollIntervalInSeconds, service.portainerURL)

	for {
		select {
		case <-pollCh:
			err := service.poll()
			if err != nil {
				log.Printf("[ERROR] [edge] [message: an error occured during short poll] [error: %s]", err)
			}
		case <-service.startSignal:
			pollCh = service.pollTicker.C
		case <-service.stopSignal:
			log.Println("[DEBUG] [edge] [message: stopping Portainer short-polling client]")
			pollCh = nil
		}
	}
}

func (service *PollService) startActivityMonitoringLoop() {
	ticker := time.NewTicker(tunnelActivityCheckInterval)

	log.Printf("[DEBUG] [edge] [monitoring_interval_seconds: %f] [inactivity_timeout: %s] [message: starting activity monitoring loop]", tunnelActivityCheckInterval.Seconds(), service.inactivityTimeout.String())

	for {
		select {
		case <-ticker.C:
			if service.lastActivity.IsZero() {
				continue
			}

			elapsed := time.Since(service.lastActivity)
			log.Printf("[DEBUG] [edge] [tunnel_last_activity_seconds: %f] [message: tunnel activity monitoring]", elapsed.Seconds())

			if service.tunnelClient != nil && service.tunnelClient.IsTunnelOpen() && elapsed.Seconds() > service.inactivityTimeout.Seconds() {
				log.Printf("[INFO] [edge] [tunnel_last_activity_seconds: %f] [message: shutting down tunnel after inactivity period]", elapsed.Seconds())

				err := service.tunnelClient.CloseTunnel()
				if err != nil {
					log.Printf("[ERROR] [edge] [message: unable to shutdown tunnel] [error: %s]", err)
				}
			}
		case <-service.updateLastActivitySignal:
			service.lastActivity = time.Now()
		}
	}
}

func (service *PollService) poll() error {
	environmentStatus, err := service.portainerClient.GetEnvironmentStatus()
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] [edge] [status: %s] [port: %d] [schedule_count: %d] [checkin_interval_seconds: %f]", environmentStatus.Status, environmentStatus.Port, len(environmentStatus.Schedules), environmentStatus.CheckinInterval)

	tunnelErr := service.manageUpdateTunnel(*environmentStatus)
	if tunnelErr != nil {
		return tunnelErr
	}

	service.processSchedules(environmentStatus.Schedules)

	if environmentStatus.CheckinInterval > 0 && environmentStatus.CheckinInterval != service.pollIntervalInSeconds {
		log.Printf("[DEBUG] [edge] [old_interval: %f] [new_interval: %f] [message: updating poll interval]", service.pollIntervalInSeconds, environmentStatus.CheckinInterval)
		service.pollIntervalInSeconds = environmentStatus.CheckinInterval
		service.portainerClient.SetTimeout(time.Duration(environmentStatus.CheckinInterval) * time.Second)
		service.pollTicker.Reset(time.Duration(service.pollIntervalInSeconds) * time.Second)
	}

	stacksErr := service.processStacks(environmentStatus.Stacks)
	if stacksErr != nil {
		return stacksErr
	}

	// TODO: REVIEW
	log.Printf("[DEBUG] [edge] [auto_update: %t] [target_version: %s]", environmentStatus.CheckForUpdate, environmentStatus.AgentTargetVersion)

	// TODO: REVIEW
	// Check the comment for the description of autoUpdateTriggered
	if environmentStatus.CheckForUpdate && !service.autoUpdateTriggered {
		log.Print("[DEBUG] [edge] [message: starting auto-update process]")
		err := service.processAutoUpdate(environmentStatus.AgentTargetVersion)
		if err != nil {
			return err
		}
	}

	return nil
}

func (service *PollService) processAutoUpdate(agentTargetVersion string) error {
	service.autoUpdateTriggered = true

	// TODO: REVIEW
	// Context should be handled properly
	ctx := context.TODO()

	// We create a Docker client to orchestrate docker operations
	cli, err := dockercli.NewClientWithOpts(dockercli.FromEnv, dockercli.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to create Docker client] [error: %s]", err)
		return err
	}
	defer cli.Close()

	// We make sure that the latest version of the portainer-updater image is available
	reader, err := cli.ImagePull(ctx, "deviantony/portainer-updater:latest", types.ImagePullOptions{})
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to pull portainer-updater Docker image] [error: %s]", err)
		return err
	}
	defer reader.Close()

	// We have to read the content of the reader otherwise the image pulling process will be asynchronous
	// This is not really well documented by the Docker SDK
	io.Copy(ioutil.Discard, reader)

	// Agent needs to retrieve its own container name to be passed to the portainer-updater service container

	// Unless overriden, the container hostname is matching the container ID
	// See https://stackoverflow.com/a/38983893

	// That could be achieved through:
	// portainerAgentContainerID, _ := os.Hostname()

	// BUT If the hostname property is set when creating the container
	// we can find ourselves in a situation where the container hostname is set to portainer_agent for example
	// but the container name / container ID is different
	// Therefore the approach of looking up the hostname is not enough.

	// Instead, we do a lookup in the /proc/1/cpuset file inside the container to find the container ID
	// See https://stackoverflow.com/a/63145861 and https://stackoverflow.com/a/25729598

	// TODO: REVIEW
	// This however will only work on Linux systems. I don't know if there is a way to do the same
	// inside a Windows container. In that case, we could fallback to the container hostname approach
	// and explicitly not support setting the hostname property on the agent container on Windows.
	cpuSetFileContent, err := os.ReadFile("/proc/1/cpuset")
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to read from /proc/1/cpuset to retrieve agent container name] [error: %s]", err)
		return err
	}

	// The content of that file looks like
	// /docker/<container ID>
	portainerAgentContainerID := strings.TrimPrefix(string(cpuSetFileContent), "/docker/")

	// Create and run the portainer-updater service container
	// docker run --rm -v /var/run/docker.sock:/var/run/docker.sock deviantony/portainer-updater agent-update portainer_agent 2.12.2

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "deviantony/portainer-updater:latest",
		Cmd:   []string{"agent-update", portainerAgentContainerID, agentTargetVersion},
	}, &container.HostConfig{
		Binds: []string{
			// TODO: REVIEW
			// This implementation will only work on Linux filesystems
			// For Windows, use a named pipe approach
			"/var/run/docker.sock:/var/run/docker.sock",
		},
	}, nil, nil, fmt.Sprintf("portainer-updater-%d", time.Now().Unix()))

	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to create portainer-updater container] [error: %s]", err)
		return err
	}

	// We then start the portainer-updater service container
	err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to start portainer-updater container] [error: %s]", err)
		return err
	}

	// We wait for it to finish
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("[ERROR] [edge] [message: an error occured while waiting for the upgrade of the agent through the portainer-updater service container] [error: %s]", err)
			return err
		}
	case <-statusCh:
	}

	// TODO: I'm not sure that this code is reachable
	// By then, I think the current agent process will be deleted.
	// Unless the existing agent is already running the latest version of the target version
	err = cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{})
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to remove portainer-updater container] [error: %s]", err)
		return err
	}

	return nil
}

func (service *PollService) manageUpdateTunnel(environmentStatus client.PollStatusResponse) error {
	if service.tunnelClient == nil {
		return nil
	}

	if environmentStatus.Status == agent.TunnelStatusIdle && service.tunnelClient.IsTunnelOpen() {
		log.Printf("[DEBUG] [edge] [status: %s] [message: Idle status detected, shutting down tunnel]", environmentStatus.Status)

		err := service.tunnelClient.CloseTunnel()
		if err != nil {
			log.Printf("[ERROR] [edge] [message: Unable to shutdown tunnel] [error: %s]", err)
		}
	}

	if environmentStatus.Status == agent.TunnelStatusRequired && !service.tunnelClient.IsTunnelOpen() {
		log.Println("[DEBUG] [edge] [message: Required status detected, creating reverse tunnel]")

		err := service.createTunnel(environmentStatus.Credentials, environmentStatus.Port)
		if err != nil {
			log.Printf("[ERROR] [edge] [message: Unable to create tunnel] [error: %s]", err)
			return err
		}
	}

	return nil
}

func (service *PollService) createTunnel(encodedCredentials string, remotePort int) error {
	decodedCredentials, err := base64.RawStdEncoding.DecodeString(encodedCredentials)
	if err != nil {
		return err
	}

	credentials, err := libcrypto.Decrypt(decodedCredentials, []byte(service.edgeID))
	if err != nil {
		return err
	}

	tunnelConfig := agent.TunnelConfig{
		LocalAddr:         service.apiServerAddr,
		ServerAddr:        service.tunnelServerAddr,
		ServerFingerprint: service.tunnelServerFingerprint,
		Credentials:       string(credentials),
		RemotePort:        strconv.Itoa(remotePort),
	}

	err = service.tunnelClient.CreateTunnel(tunnelConfig)
	if err != nil {
		return err
	}

	service.resetActivityTimer()
	return nil
}

func (service *PollService) processSchedules(schedules []agent.Schedule) {
	err := service.scheduleManager.Schedule(schedules)
	if err != nil {
		log.Printf("[ERROR] [edge] [message: an error occurred during schedule management] [err: %s]", err)
	}

	logsToCollect := []int{}
	for _, schedule := range schedules {
		if schedule.CollectLogs {
			logsToCollect = append(logsToCollect, schedule.ID)
		}
	}

	service.logsManager.HandleReceivedLogsRequests(logsToCollect)
}

func (service *PollService) processStacks(pollResponseStacks []client.StackStatus) error {
	if pollResponseStacks == nil {
		return nil
	}

	stacks := map[int]int{}
	for _, s := range pollResponseStacks {
		stacks[s.ID] = s.Version
	}

	err := service.edgeStackManager.UpdateStacksStatus(stacks)
	if err != nil {
		log.Printf("[ERROR] [edge] [message: an error occurred during stack management] [error: %s]", err)
		return err
	}
	return nil
}
