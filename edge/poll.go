package edge

import (
	"encoding/base64"
	"log"
	"strconv"
	"time"

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
// It is responsible for managing the state of the reverse tunnel (open and closing after inactivity).
// It is also responsible for retrieving the data associated to Edge stacks and schedules.
type PollService struct {
	apiServerAddr            string
	pollIntervalInSeconds    float64
	inactivityTimeout        time.Duration
	edgeID                   string
	portainerClient          client.PortainerClient
	tunnelClient             agent.ReverseTunnelClient
	scheduleManager          agent.Scheduler
	lastActivity             time.Time
	updateLastActivitySignal chan struct{}
	refreshSignal            chan struct{}
	edgeStackManager         *stack.StackManager
	portainerURL             string
	tunnelServerAddr         string
	tunnelServerFingerprint  string
	logsManager              *scheduler.LogsManager
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
	ContainerPlatform       agent.ContainerPlatform
}

// newPollService returns a pointer to a new instance of PollService
// if TunnelCapability is disabled, it will only poll for Edge stacks and schedule without managing reverse tunnels.
func newPollService(edgeStackManager *stack.StackManager, logsManager *scheduler.LogsManager, config *pollServiceConfig, portainerClient client.PortainerClient) (*PollService, error) {
	pollFrequency, err := time.ParseDuration(config.PollFrequency)
	if err != nil {
		return nil, err
	}

	inactivityTimeout, err := time.ParseDuration(config.InactivityTimeout)
	if err != nil {
		return nil, err
	}

	var tunnel agent.ReverseTunnelClient
	if config.TunnelCapability {
		tunnel = chisel.NewClient()
	}

	return &PollService{
		pollIntervalInSeconds:    pollFrequency.Seconds(),
		inactivityTimeout:        inactivityTimeout,
		scheduleManager:          scheduler.NewCronManager(),
		updateLastActivitySignal: make(chan struct{}),
		edgeStackManager:         edgeStackManager,
		logsManager:              logsManager,
		tunnelClient:             tunnel,
		portainerClient:          portainerClient,
		edgeID:                   config.EdgeID,
		portainerURL:             config.PortainerURL,
		apiServerAddr:            config.APIServerAddr,
		tunnelServerAddr:         config.TunnelServerAddr,
		tunnelServerFingerprint:  config.TunnelServerFingerprint,
	}, nil
}

func (service *PollService) resetActivityTimer() {
	if service.tunnelClient != nil && service.tunnelClient.IsTunnelOpen() {
		service.updateLastActivitySignal <- struct{}{}
	}
}

// start will start two loops in go routines
// The first loop will poll the Portainer instance for the status of the associated endpoint and create a reverse tunnel
// if needed as well as manage schedules.
// The second loop will check for the last activity of the reverse tunnel and close the tunnel if it exceeds the tunnel
// inactivity duration.
func (service *PollService) start() error {
	if service.refreshSignal != nil {
		return nil
	}
	service.refreshSignal = make(chan struct{})

	service.startStatusPollLoop()
	go service.startActivityMonitoringLoop()

	return nil
}

func (service *PollService) stop() {
	if service.refreshSignal == nil {
		return
	}
	close(service.refreshSignal)
	service.refreshSignal = nil
}

func (service *PollService) restartStatusPollLoop() {
	service.stop()
	service.refreshSignal = make(chan struct{})
	service.startStatusPollLoop()
}

func (service *PollService) startStatusPollLoop() {
	log.Printf("[DEBUG] [edge] [poll_interval_seconds: %f] [server_url: %s] [message: starting Portainer short-polling client]", service.pollIntervalInSeconds, service.portainerURL)

	ticker := time.NewTicker(time.Duration(service.pollIntervalInSeconds) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := service.poll()
				if err != nil {
					log.Printf("[ERROR] [edge] [message: an error occured during short poll] [error: %s]", err)
				}

			case <-service.refreshSignal:
				log.Println("[DEBUG] [edge] [message: shutting down Portainer short-polling client]")
				ticker.Stop()
				return
			}
		}
	}()
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

	log.Printf("[DEBUG] [edge] [status: %s] [port: %d] [schedule_count: %d] [checkin_interval_seconds: %f]", environmentStatus.Status, environmentStatus.Port, len(environmentStatus.Schedules), environmentStatus.CheckinInterval)

	if service.tunnelClient != nil {
		if environmentStatus.Status == "IDLE" && service.tunnelClient.IsTunnelOpen() {
			log.Printf("[DEBUG] [edge] [status: %s] [message: Idle status detected, shutting down tunnel]", environmentStatus.Status)

			err := service.tunnelClient.CloseTunnel()
			if err != nil {
				log.Printf("[ERROR] [edge] [message: Unable to shutdown tunnel] [error: %s]", err)
			}
		}

		if environmentStatus.Status == "REQUIRED" && !service.tunnelClient.IsTunnelOpen() {
			log.Println("[DEBUG] [edge] [message: Required status detected, creating reverse tunnel]")

			err := service.createTunnel(environmentStatus.Credentials, environmentStatus.Port)
			if err != nil {
				log.Printf("[ERROR] [edge] [message: Unable to create tunnel] [error: %s]", err)
				return err
			}
		}
	}

	err = service.scheduleManager.Schedule(environmentStatus.Schedules)
	if err != nil {
		log.Printf("[ERROR] [edge] [message: an error occurred during schedule management] [err: %s]", err)
	}

	logsToCollect := []int{}
	for _, schedule := range environmentStatus.Schedules {
		if schedule.CollectLogs {
			logsToCollect = append(logsToCollect, schedule.ID)
		}
	}

	service.logsManager.HandleReceivedLogsRequests(logsToCollect)

	if environmentStatus.CheckinInterval != service.pollIntervalInSeconds {
		log.Printf("[DEBUG] [edge] [old_interval: %f] [new_interval: %f] [message: updating poll interval]", service.pollIntervalInSeconds, environmentStatus.CheckinInterval)
		service.pollIntervalInSeconds = environmentStatus.CheckinInterval
		service.portainerClient.SetTimeout(time.Duration(environmentStatus.CheckinInterval) * time.Second)
		go service.restartStatusPollLoop()
	}

	if environmentStatus.Stacks != nil {
		stacks := map[int]int{}
		for _, stack := range environmentStatus.Stacks {
			stacks[stack.ID] = stack.Version
		}

		err := service.edgeStackManager.UpdateStacksStatus(stacks)
		if err != nil {
			log.Printf("[ERROR] [edge] [message: an error occurred during stack management] [error: %s]", err)
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
