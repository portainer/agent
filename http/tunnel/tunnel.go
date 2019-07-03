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

const clientPollTimeout = 3
const tunnelActivityCheckInterval = 30 * time.Second

type edgeKey struct {
	PortainerInstanceURL    string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	EndpointID              string
	Credentials             string
}

// Operator is used to poll a Portainer instance and to establish a reverse tunnel if needed.
// It also takes care of closing the tunnel after a set period of inactivity.
type Operator struct {
	apiServerAddr   string
	pollInterval    time.Duration
	sleepInterval   time.Duration
	edgeID          string
	key             *edgeKey
	httpClient      *http.Client
	tunnelClient    agent.ReverseTunnelClient
	scheduleManager agent.Scheduler
	lastActivity    time.Time
}

// NewTunnelOperator creates a new reverse tunnel operator
func NewTunnelOperator(apiServerAddr, edgeID, pollInterval, sleepInterval string) (*Operator, error) {
	pollDuration, err := time.ParseDuration(pollInterval)
	if err != nil {
		return nil, err
	}

	sleepDuration, err := time.ParseDuration(sleepInterval)
	if err != nil {
		return nil, err
	}

	return &Operator{
		apiServerAddr: apiServerAddr,
		edgeID:        edgeID,
		pollInterval:  pollDuration,
		sleepInterval: sleepDuration,
		httpClient: &http.Client{
			Timeout: time.Second * clientPollTimeout,
		},
		tunnelClient:    chisel.NewClient(),
		scheduleManager: filesystem.NewCronManager(),
	}, nil
}

// SetKey parses and associate a key to the operator
func (operator *Operator) SetKey(key string) error {
	edgeKey, err := parseEdgeKey(key)
	if err != nil {
		return err
	}

	// TODO: @@DOCUMENTATION
	// Add documentation about key persistence
	err = filesystem.WriteFile(agent.DataDirectory, agent.EdgeKeyFile, []byte(key), 0444)
	if err != nil {
		return err
	}

	operator.key = edgeKey

	return nil
}

// GetKey returns the key associated to the operator
func (operator *Operator) GetKey() string {
	var encodedKey string

	if operator.key != nil {
		encodedKey = encodeKey(operator.key)
	}

	return encodedKey
}

// IsKeySet checks if a key is associated to the operator
func (operator *Operator) IsKeySet() bool {
	if operator.key == nil {
		return false
	}
	return true
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
	if operator.key == nil {
		return errors.New("missing Edge key")
	}

	operator.startStatusPollLoop()
	operator.startActivityMonitoringLoop()

	return nil
}

func (operator *Operator) startStatusPollLoop() {
	ticker := time.NewTicker(operator.pollInterval)
	quit := make(chan struct{})

	log.Printf("[DEBUG] [http,edge,poll] [poll_interval: %s] [server_url: %s] [message: Starting Portainer short-polling client]", operator.pollInterval.String(), operator.key.PortainerInstanceURL)

	go func() {
		for {
			select {
			case <-ticker.C:
				err := operator.poll()
				if err != nil {
					log.Printf("[ERROR] [edge,http,poll] [message: An error occured during short poll] [error: %s]", err)
				}

			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (operator *Operator) startActivityMonitoringLoop() {
	ticker := time.NewTicker(tunnelActivityCheckInterval)
	quit := make(chan struct{})

	log.Printf("[DEBUG] [http,edge,monitoring] [monitoring_interval_seconds: %f] [inactivity_timeout: %s] [message: Starting activity monitoring loop]", tunnelActivityCheckInterval.Seconds(), operator.sleepInterval.String())

	go func() {
		for {
			select {
			case <-ticker.C:

				if operator.lastActivity.IsZero() {
					continue
				}

				elapsed := time.Since(operator.lastActivity)
				log.Printf("[DEBUG] [http,edge,monitoring] [tunnel_last_activity_seconds: %f] [message: tunnel activity monitoring]", elapsed.Seconds())

				if operator.tunnelClient.IsTunnelOpen() && elapsed.Seconds() > operator.sleepInterval.Seconds() {

					log.Printf("[INFO] [http,edge,monitoring] [tunnel_last_activity_seconds: %f] [message: Shutting down tunnel after inactivity period]", elapsed.Seconds())

					err := operator.tunnelClient.CloseTunnel()
					if err != nil {
						log.Printf("[ERROR] [http,edge,monitoring] [message: Unable to shutdown tunnel] [error: %s]", err)
					}
				}

			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}
