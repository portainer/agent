package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/portainer/agent"
)

type portainerResponse struct {
	Status string `json:"status"`
	Port   int    `json:"port"`
}

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

	// if responsecode != 200 { log message }
	// if status == IDLE { do nothing }
	// if status == REQUIRED and tunnel closed { create tunnel }
	// if status == ACTIVE and tunnel closed { create tunnel }

	if respData.Status != "IDLE" && !operator.tunnelClient.IsTunnelOpen() {
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

	return nil
}
