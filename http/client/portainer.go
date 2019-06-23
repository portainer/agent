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
	pollInterval        string
	key                 *edgeKey
	tunnelServerAddress string

	httpClient   *http.Client
	tunnelClient agent.ReverseTunnelClient
}

// NewTunnelOperator creates a new reverse tunnel operator
// It stores the specified tunnel server address and uses it
// if the server address specified in the key equals to localhost
// TODO: this tunnel server address override is a work-around for a problem on Portainer side.
// The server address is currently retrieved from the browser host when creating an endpoint inside Portainer
// and can be equal to localhost when using a local deployment of Portainer (http://localhost:9000)
// This override can be set via the EDGE_TUNNEL_SERVER env var.
// This should be documented in the README or simply prevent the use of Edge when connected to a localhost instance.
func NewTunnelOperator(tunnelServerAddr, pollInterval string) *TunnelOperator {
	return &TunnelOperator{
		tunnelServerAddress: tunnelServerAddr,
		pollInterval:        pollInterval,
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
	log.Printf("[DEBUG] [http,edge] [poll_interval: %s] [message: Starting Portainer short-polling client]", pollInterval.String())

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
	edgeKey, err := parseEdgeKey(key, operator.tunnelServerAddress)
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
