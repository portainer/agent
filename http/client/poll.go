package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"
)

// TODO: dependency on Portainer
// remove by re-creating struct with required fields only?
type portainerResponse struct {
	Status    string                   `json:"status"`
	Port      int                      `json:"port"`
	Schedules []portainer.EdgeSchedule `json:"Schedules"`
}

// TODO: scheduling thoughts
// In order to run on each node inside a Swarm cluster
// poll() must run on each agent, not only on the one that manages the Edge startup
//
// this implementation is gonna leverage cron which might not be available on all the systems
// e.g. windows. Schedule management should be skipped on Windows platforms.

// TODO: refactor/rewrite
func (operator *TunnelOperator) poll() error {

	if operator.key == nil {
		return errors.New("missing Edge key")
	}

	portainerURL := fmt.Sprintf("%s/api/endpoints/%s/status", operator.key.PortainerInstanceURL, operator.key.EndpointID)
	resp, err := operator.httpClient.Get(portainerURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var respData portainerResponse
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		return err
	}

	// TODO: better DEBUG messages
	log.Printf("[DEBUG] [http,edge,poll] [portainer_poll_response: %+v]", respData)

	if respData.Status == "REQUIRED" && !operator.tunnelClient.IsTunnelOpen() {
		tunnelConfig := agent.TunnelConfig{
			ServerAddr:       operator.key.TunnelServerAddr,
			ServerFingerpint: operator.key.TunnelServerFingerprint,
			Credentials:      operator.key.Credentials,
			RemotePort:       strconv.Itoa(respData.Port),
		}

		log.Printf("[DEBUG] [http,edge,poll] [status: %s] [port: %d] [message: active status, will create tunnel]", respData.Status, respData.Port)

		err = operator.tunnelClient.CreateTunnel(tunnelConfig)
		if err != nil {
			return err
		}
	}

	// TODO: ignore on Windows platform
	schedules := make([]agent.CronSchedule, 0)
	for _, edgeSchedule := range respData.Schedules {

		schedule := agent.CronSchedule{
			ID:             int(edgeSchedule.ID),
			CronExpression: edgeSchedule.CronExpression,
			Script:         edgeSchedule.Script,
			//ScriptHash:     edgeSchedule.ScriptHash,
		}

		schedules = append(schedules, schedule)
	}

	// TODO: maybe make cronManager_linux.go and cronManger_windows.go (empty schedule function)
	err = operator.cronManager.Schedule(schedules)
	if err != nil {
		log.Printf("[ERROR] [http,edge,cron] [message: an error occured during schedule management] [err: %s]", err)
	}

	return nil
}
