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
	pollInterval  time.Duration
	sleepInterval time.Duration
	key           *edgeKey
	httpClient    *http.Client
	tunnelClient  agent.ReverseTunnelClient
	lastActivity  time.Time
}

// NewTunnelOperator creates a new reverse tunnel operator
func NewTunnelOperator(pollInterval, sleepInterval string) (*TunnelOperator, error) {
	pollDuration, err := time.ParseDuration(pollInterval)
	if err != nil {
		return nil, err
	}

	sleepDuration, err := time.ParseDuration(sleepInterval)
	if err != nil {
		return nil, err
	}

	return &TunnelOperator{
		pollInterval:  pollDuration,
		sleepInterval: sleepDuration,
		httpClient: &http.Client{
			Timeout: time.Second * clientPollTimeout,
		},
		tunnelClient: chisel.NewClient(),
	}, nil
}

// TODO: doc
// + refactor
func (operator *TunnelOperator) Start() error {
	log.Printf("[DEBUG] [http,edge] [poll_interval: %s] [server_url: %s] [sleep_interval: %s] [message: Starting Portainer short-polling client]", operator.pollInterval.String(), operator.key.PortainerInstanceURL, operator.sleepInterval.String())

	// TODO: ping before starting poll loop?

	ticker := time.NewTicker(operator.pollInterval)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				// TODO: start in a go routine?
				// If not, can delay other task executions based on the time spent in last exec
				// https://stackoverflow.com/questions/16466320/is-there-a-way-to-do-repetitive-tasks-at-intervals-in-golang#comment52389800_16466581
				err := operator.poll()
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

	// TODO: other ticker value?
	ticker2 := time.NewTicker(10 * time.Second)
	quit2 := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker2.C:
				elapsed := time.Since(operator.lastActivity)
				if operator.tunnelClient.IsTunnelOpen() && !operator.lastActivity.IsZero() && elapsed.Seconds() > operator.sleepInterval.Seconds() {
					log.Println("[INFO] [http,edge,rtunnel] [message: Shutting down tunnel after inactivity period]")
					err := operator.tunnelClient.CloseTunnel()
					if err != nil {
						log.Printf("[ERROR] [http,edge,rtunnel] [message: Unable to shut down tunnel] [error: %s]", err)
					}
				}

			// do something
			case <-quit2:
				ticker2.Stop()
				return
			}
		}
	}()

	// TODO: required?
	// close(quit2) to exit

	// TODO: put this in a goroutine and poll loop not in go routine?
	//operator.activityTimer = time.NewTimer(operator.sleepInterval)
	//cancel := make(chan struct{})

	//go func() {
	//	for {
	//		select {
	//		case <-operator.activityTimer.C:
	//			// TODO: will only do this once, must implement sleep/wake loop
	//			if operator.tunnelClient.IsTunnelOpen() {
	//				log.Println("[INFO] [http,edge,rtunnel] [message: Shutting down tunnel after inactivity period]")
	//				err := operator.tunnelClient.CloseTunnel()
	//				if err != nil {
	//					log.Printf("[ERROR] [http,edge,rtunnel] [message: Unable to shut down tunnel] [error: %s]", err)
	//					// TODO: log
	//					//return err
	//				}
	//			}
	//
	//			operator.ResetActivityTimer()
	//		case <-cancel:
	//			return
	//		}
	//	}
	//}()

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

func (operator *TunnelOperator) ResetActivityTimer() {
	//if !operator.activityTimer.Stop() {
	//	<-operator.activityTimer.C
	//}
	//operator.activityTimer.Reset(operator.sleepInterval)

	operator.lastActivity = time.Now()
}
