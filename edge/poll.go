package edge

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/mitchellh/mapstructure"

	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	"github.com/portainer/libcrypto"
)

const (
	tunnelActivityCheckInterval = 30 * time.Second
)

// PollService is used to poll a Portainer instance to retrieve the status associated to the Edge endpoint.
// It is responsible for managing the state of the reverse tunnel (open and closing after inactivity).
// It is also responsible for retrieving the data associated to Edge stacks and schedules.
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
	edgeManager              *Manager
	edgeStackManager         *stack.StackManager
	portainerURL             string
	tunnelServerAddr         string
	tunnelServerFingerprint  string
}

type pollServiceConfig struct {
	APIServerAddr           string
	EdgeID                  string
	InactivityTimeout       string
	PollFrequency           string
	TunnelCapability        bool
	PortainerURL            string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	ContainerPlatform       agent.ContainerPlatform
}

// newPollService returns a pointer to a new instance of PollService, and will start two loops in go routines.
// The first loop will poll the Portainer instance for the status of the associated endpoint and create a reverse tunnel
// if needed as well as manage schedules.
// The second loop will check for the last activity of the reverse tunnel and close the tunnel if it exceeds the tunnel
// inactivity duration.
// If TunnelCapability is disabled, it will only poll for Edge stacks and schedule without managing reverse tunnels.
func newPollService(edgeManager *Manager, edgeStackManager *stack.StackManager, logsManager *scheduler.LogsManager, config *pollServiceConfig, portainerClient client.PortainerClient) (*PollService, error) {
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
		scheduleManager:          scheduler.NewCronManager(logsManager),
		updateLastActivitySignal: make(chan struct{}),
		startSignal:              make(chan struct{}),
		stopSignal:               make(chan struct{}),
		edgeManager:              edgeManager,
		edgeStackManager:         edgeStackManager,
		portainerURL:             config.PortainerURL,
		tunnelServerAddr:         config.TunnelServerAddr,
		tunnelServerFingerprint:  config.TunnelServerFingerprint,
		portainerClient:          portainerClient,
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
	if service.edgeManager.GetEndpointID() == 0 {
		endpointID, err := service.portainerClient.GetEnvironmentID()
		if err != nil {
			return err
		}

		service.edgeManager.SetEndpointID(endpointID)
	}

	environmentStatus, err := service.portainerClient.GetEnvironmentStatus()
	if err != nil {
		return err
	}

	if environmentStatus.Status == agent.TunnelStatusNoTunnel {
		err = service.processAsyncCommands(environmentStatus.AsyncCommands)
		if err != nil {
			return err
		}

		service.scheduleManager.ProcessScheduleLogsCollection()
		return nil
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

func (service *PollService) processAsyncCommands(commands []client.AsyncCommand) error {
	ctx := context.Background()

	for _, command := range commands {
		switch command.Type {
		case "edgeStack":
			err := service.processStackCommand(ctx, command)
			if err != nil {
				return err
			}
			break
		case "edgeJob":
			err := service.processScheduleCommand(command)
			if err != nil {
				return err
			}
			break
		default:
			return fmt.Errorf("command type %v not supported", command.Type)
		}
		service.portainerClient.SetLastCommandTimestamp(command.Timestamp)
	}
	return nil
}

func (service *PollService) processStackCommand(ctx context.Context, command client.AsyncCommand) error {
	var stackData client.EdgeStackData
	err := mapstructure.Decode(command.Value, &stackData)
	if err != nil {
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to edgeStackData", command.Value)
		return err
	}

	if command.Operation == "add" || command.Operation == "replace" {
		responseStatus := int(stack.EdgeStackStatusOk)
		errorMessage := ""

		err = service.edgeStackManager.DeployStack(ctx, stackData)
		if err != nil {
			responseStatus = int(stack.EdgeStackStatusError)
			errorMessage = err.Error()
		}

		return service.portainerClient.SetEdgeStackStatus(stackData.ID, responseStatus, errorMessage)
	}

	if command.Operation == "remove" {
		responseStatus := int(stack.EdgeStackStatusRemove)
		errorMessage := ""

		err = service.edgeStackManager.DeleteStack(ctx, stackData)
		if err != nil {
			responseStatus = int(stack.EdgeStackStatusError)
			errorMessage = err.Error()
		}

		return service.portainerClient.SetEdgeStackStatus(stackData.ID, responseStatus, errorMessage)
	}

	return fmt.Errorf("operation %v not supported", command.Operation)
}

func (service *PollService) processScheduleCommand(command client.AsyncCommand) error {
	var jobData client.EdgeJobData
	err := mapstructure.Decode(command.Value, &jobData)
	if err != nil {
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to edgeStackData", command.Value)
		return err
	}

	schedule := agent.Schedule{
		ID:             int(jobData.ID),
		CronExpression: jobData.CronExpression,
		Script:         jobData.ScriptFileContent,
		Version:        jobData.Version,
		CollectLogs:    jobData.CollectLogs,
	}

	if command.Operation == "add" || command.Operation == "replace" {
		err = service.scheduleManager.AddSchedule(schedule)
		if err != nil {
			log.Printf("[ERROR] [edge] [message: error adding schedule] [error: %s]", err)
		}
		return nil
	}

	if command.Operation == "remove" {
		err = service.scheduleManager.RemoveSchedule(schedule)
		if err != nil {
			log.Printf("[ERROR] [edge] [message: error removing schedule] [error: %s]", err)
		}
		return nil
	}

	return fmt.Errorf("operation %v not supported", command.Operation)
}
