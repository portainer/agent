package tunnel

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/filesystem"
)

const tunnelActivityCheckInterval = 30 * time.Second

// Operator is used to poll a Portainer instance and to establish a reverse tunnel if needed.
// It also takes care of closing the tunnel after a set period of inactivity.
type Operator struct {
	apiServerAddr         string
	pollIntervalInSeconds float64
	insecurePoll          bool
	inactivityTimeout     time.Duration
	edgeID                string
	httpClient            *http.Client
	tunnelClient          agent.ReverseTunnelClient
	scheduleManager       agent.Scheduler
	lastActivity          time.Time
	refreshSignal         chan struct{}
	// edgeStackManager      agent.EdgeStackManager
	edgeKeyService agent.EdgeKeyService
}

// OperatorConfig represents the configuration used to create a new Operator.
type OperatorConfig struct {
	APIServerAddr     string
	EdgeID            string
	InactivityTimeout string
	PollFrequency     string
	InsecurePoll      bool
}

// NewTunnelOperator creates a new reverse tunnel operator
func NewTunnelOperator(edgeKeyService agent.EdgeKeyService, config *OperatorConfig) (*Operator, error) {
	pollFrequency, err := time.ParseDuration(config.PollFrequency)
	if err != nil {
		return nil, err
	}

	inactivityTimeout, err := time.ParseDuration(config.InactivityTimeout)
	if err != nil {
		return nil, err
	}

	return &Operator{
		apiServerAddr:         config.APIServerAddr,
		edgeID:                config.EdgeID,
		pollIntervalInSeconds: pollFrequency.Seconds(),
		insecurePoll:          config.InsecurePoll,
		inactivityTimeout:     inactivityTimeout,
		tunnelClient:          chisel.NewClient(),
		scheduleManager:       filesystem.NewCronManager(),
		refreshSignal:         nil,
		edgeKeyService:        edgeKeyService,
		// edgeStackManager:      edgeStackManager,
	}, nil
}

// CloseTunnel closes the reverse tunnel managed by the operator
func (operator *Operator) CloseTunnel() error {
	return operator.tunnelClient.CloseTunnel()
}

// ResetActivityTimer will reset the last activity time timer
func (operator *Operator) ResetActivityTimer() {
	if operator.tunnelClient.IsTunnelOpen() {
		operator.lastActivity = time.Now()
	}
}

// Start will start two loops in go routines
// The first loop will poll the Portainer instance for the status of the associated endpoint and create a reverse tunnel
// if needed as well as manage schedules.
// The second loop will check for the last activity of the reverse tunnel and close the tunnel if it exceeds the tunnel
// inactivity duration.
func (operator *Operator) Start() error {
	if !operator.edgeKeyService.IsKeySet() {
		return errors.New("missing Edge key")
	}
	if operator.refreshSignal != nil {
		return nil
	}
	operator.refreshSignal = make(chan struct{})
	operator.startStatusPollLoop()
	operator.startActivityMonitoringLoop()

	return nil
}

// Stop stops the poll loop
func (operator *Operator) Stop() error {
	if operator.refreshSignal != nil {
		close(operator.refreshSignal)
		operator.refreshSignal = nil
	}
	return nil
}

func (operator *Operator) restartStatusPollLoop() {
	operator.Stop()
	operator.startStatusPollLoop()
}

func (operator *Operator) startStatusPollLoop() error {
	portainerURL, _, err := operator.edgeKeyService.GetPortainerConfig()
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] [http,edge,poll] [poll_interval_seconds: %f] [server_url: %s] [message: starting Portainer short-polling client]", operator.pollIntervalInSeconds, portainerURL)

	ticker := time.NewTicker(time.Duration(operator.pollIntervalInSeconds) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := operator.poll()
				if err != nil {
					log.Printf("[ERROR] [edge,http,poll] [message: an error occured during short poll] [error: %s]", err)
				}

			case <-operator.refreshSignal:
				log.Println("[DEBUG] [http,edge,poll] [message: shutting down Portainer short-polling client]")
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

func (operator *Operator) startActivityMonitoringLoop() {
	ticker := time.NewTicker(tunnelActivityCheckInterval)
	quit := make(chan struct{})

	log.Printf("[DEBUG] [http,edge,monitoring] [monitoring_interval_seconds: %f] [inactivity_timeout: %s] [message: starting activity monitoring loop]", tunnelActivityCheckInterval.Seconds(), operator.inactivityTimeout.String())

	go func() {
		for {
			select {
			case <-ticker.C:

				if operator.lastActivity.IsZero() {
					continue
				}

				elapsed := time.Since(operator.lastActivity)
				log.Printf("[DEBUG] [http,edge,monitoring] [tunnel_last_activity_seconds: %f] [message: tunnel activity monitoring]", elapsed.Seconds())

				if operator.tunnelClient.IsTunnelOpen() && elapsed.Seconds() > operator.inactivityTimeout.Seconds() {

					log.Printf("[INFO] [http,edge,monitoring] [tunnel_last_activity_seconds: %f] [message: shutting down tunnel after inactivity period]", elapsed.Seconds())

					err := operator.tunnelClient.CloseTunnel()
					if err != nil {
						log.Printf("[ERROR] [http,edge,monitoring] [message: unable to shutdown tunnel] [error: %s]", err)
					}
				}

			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}
