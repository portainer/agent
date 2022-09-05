package edge

import (
	"encoding/base64"
	"strconv"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	"github.com/portainer/libcrypto"

	"github.com/rs/zerolog/log"
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

	// Async mode only
	pingInterval     time.Duration
	snapshotInterval time.Duration
	commandInterval  time.Duration
	pingTicker       *time.Ticker
	snapshotTicker   *time.Ticker
	commandTicker    *time.Ticker
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
func newPollService(edgeManager *Manager, edgeStackManager *stack.StackManager, logsManager *scheduler.LogsManager, config *pollServiceConfig, portainerClient client.PortainerClient, edgeAsyncMode bool) (*PollService, error) {
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

	if edgeAsyncMode {
		go pollService.startStatusPollLoopAsync()
	} else {
		pollService.pollTicker = time.NewTicker(pollFrequency)

		go pollService.startStatusPollLoop()
		go pollService.startActivityMonitoringLoop()
	}

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

	log.Debug().
		Float64("poll_interval_seconds", service.pollIntervalInSeconds).
		Str("server_url", service.portainerURL).
		Msg("starting Portainer short-polling client")

	for {
		select {
		case <-pollCh:
			err := service.poll()
			if err != nil {
				log.Error().Err(err).Msg("an error occured during short poll")
			}
		case <-service.startSignal:
			pollCh = service.pollTicker.C
		case <-service.stopSignal:
			log.Debug().Msg("stopping Portainer short-polling client")

			pollCh = nil
		}
	}
}

func (service *PollService) startActivityMonitoringLoop() {
	ticker := time.NewTicker(tunnelActivityCheckInterval)

	log.Debug().
		Float64("monitoring_interval_seconds", tunnelActivityCheckInterval.Seconds()).
		Str("inactivity_timeout", service.inactivityTimeout.String()).
		Msg("")

	for {
		select {
		case <-ticker.C:
			if service.lastActivity.IsZero() {
				continue
			}

			elapsed := time.Since(service.lastActivity)

			log.Debug().
				Float64("tunnel_last_activity_seconds", elapsed.Seconds()).
				Msg("tunnel activity monitoring")

			if service.tunnelClient != nil && service.tunnelClient.IsTunnelOpen() && elapsed.Seconds() > service.inactivityTimeout.Seconds() {
				log.Info().
					Float64("tunnel_last_activity_seconds", elapsed.Seconds()).
					Msg("shutting down tunnel after inactivity period")

				err := service.tunnelClient.CloseTunnel()
				if err != nil {
					log.Error().Err(err).Msg("unable to shutdown tunnel")
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

	log.Debug().
		Str("status", environmentStatus.Status).
		Int("port", environmentStatus.Port).
		Int("schedule_count", len(environmentStatus.Schedules)).
		Float64("checkin_interval_seconds", environmentStatus.CheckinInterval).
		Msg("")

	tunnelErr := service.manageUpdateTunnel(*environmentStatus)
	if tunnelErr != nil {
		return tunnelErr
	}

	service.processSchedules(environmentStatus.Schedules)

	if environmentStatus.CheckinInterval > 0 && environmentStatus.CheckinInterval != service.pollIntervalInSeconds {
		log.Debug().
			Float64("old_interval", service.pollIntervalInSeconds).
			Float64("new_interval", environmentStatus.CheckinInterval).
			Msg("updating poll interval")

		service.pollIntervalInSeconds = environmentStatus.CheckinInterval
		service.portainerClient.SetTimeout(time.Duration(environmentStatus.CheckinInterval) * time.Second)
		service.pollTicker.Reset(time.Duration(service.pollIntervalInSeconds) * time.Second)
	}

	return service.processStacks(environmentStatus.Stacks)
}

func (service *PollService) manageUpdateTunnel(environmentStatus client.PollStatusResponse) error {
	if service.tunnelClient == nil {
		return nil
	}

	if environmentStatus.Status == agent.TunnelStatusIdle && service.tunnelClient.IsTunnelOpen() {
		log.Debug().
			Str("status", environmentStatus.Status).
			Msg("idle status detected, shutting down tunnel")

		err := service.tunnelClient.CloseTunnel()
		if err != nil {
			log.Error().Err(err).Msg("unable to shutdown tunnel")
		}
	}

	if environmentStatus.Status == agent.TunnelStatusRequired && !service.tunnelClient.IsTunnelOpen() {
		log.Debug().Msg("required status detected, creating reverse tunnel")

		err := service.createTunnel(environmentStatus.Credentials, environmentStatus.Port)
		if err != nil {
			log.Error().Err(err).Msg("unable to create tunnel")

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
		log.Error().Err(err).Msg("an error occurred during schedule management")
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
		log.Error().Err(err).Msg("an error occurred during stack management")

		return err
	}

	return nil
}
