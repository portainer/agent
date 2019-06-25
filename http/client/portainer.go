package client

import (
	"log"
	"net/http"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
)

const clientPollTimeout = 3

type edgeKey struct {
	PortainerInstanceURL    string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	EndpointID              string
	Credentials             string
}

type TunnelOperator struct {
	pollInterval string
	key          *edgeKey
	httpClient   *http.Client
	tunnelClient agent.ReverseTunnelClient
}

// NewTunnelOperator creates a new reverse tunnel operator
func NewTunnelOperator(pollInterval string) *TunnelOperator {
	return &TunnelOperator{
		pollInterval: pollInterval,
		httpClient: &http.Client{
			Timeout: time.Second * clientPollTimeout,
		},
		tunnelClient: chisel.NewClient(),
	}
}

// TODO: doc
func (operator *TunnelOperator) Start() error {
	pollInterval, err := time.ParseDuration(operator.pollInterval)
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] [http,edge] [poll_interval: %s] [server_url: %s] [message: Starting Portainer short-polling client]", pollInterval.String(), operator.key.PortainerInstanceURL)

	// TODO: ping before starting poll loop?

	ticker := time.NewTicker(pollInterval)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				// TODO: start in a go routine?
				// If not, can delay other task executions based on the time spent in last exec
				// https://stackoverflow.com/questions/16466320/is-there-a-way-to-do-repetitive-tasks-at-intervals-in-golang#comment52389800_16466581
				err = operator.poll()
				if err != nil {
					log.Printf("[ERROR] [edge,http] [message: An error occured during short poll] [error: %s]", err)
				}

			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// TODO: required?
	// close(quit) to exit
	return nil
}

// SetKey parses and associate a key to the operator
// TODO: key should be persisted
func (operator *TunnelOperator) SetKey(key string) error {
	edgeKey, err := parseEdgeKey(key)
	if err != nil {
		return err
	}

	operator.key = edgeKey

	return nil
}

// IsKeySet checks if a key is associated to the operator
func (operator *TunnelOperator) IsKeySet() bool {
	if operator.key == nil {
		return false
	}
	return true
}

// CloseTunnel closes the reverse tunnel managed by the operator
func (operator *TunnelOperator) CloseTunnel() error {
	return operator.tunnelClient.CloseTunnel()
}
