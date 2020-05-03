package tunnel

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/libcrypto"
)

const clientDefaultPollTimeout = 5

type stackStatus struct {
	ID      int
	Version int
}

type pollStatusResponse struct {
	Status          string           `json:"status"`
	Port            int              `json:"port"`
	Schedules       []agent.Schedule `json:"schedules"`
	CheckinInterval float64          `json:"checkin"`
	Credentials     string           `json:"credentials"`
	Stacks          []stackStatus    `json:"stacks"`
}

func (operator *Operator) createHTTPClient(timeout float64) {
	httpCli := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	if operator.insecurePoll {
		httpCli.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	operator.httpClient = httpCli
}

func (operator *Operator) poll() error {
	portainerURL, endpointID, err := operator.edgeKeyService.GetPortainerConfig()
	if err != nil {
		return err
	}

	pollURL := fmt.Sprintf("%s/api/endpoints/%s/status", portainerURL, endpointID)
	req, err := http.NewRequest("GET", pollURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, operator.edgeID)

	if operator.httpClient == nil {
		operator.createHTTPClient(clientDefaultPollTimeout)
	}

	resp, err := operator.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[DEBUG] [http,edge,poll] [response_code: %d] [message: Poll request failure]", resp.StatusCode)
		return errors.New("short poll request failed")
	}

	var responseData pollStatusResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] [http,edge,poll] [status: %s] [port: %d] [schedule_count: %d] [checkin_interval_seconds: %f]", responseData.Status, responseData.Port, len(responseData.Schedules), responseData.CheckinInterval)

	if responseData.Status == "IDLE" && operator.tunnelClient.IsTunnelOpen() {
		log.Printf("[DEBUG] [http,edge,poll] [status: %s] [message: Idle status detected, shutting down tunnel]", responseData.Status)

		err := operator.tunnelClient.CloseTunnel()
		if err != nil {
			log.Printf("[ERROR] [http,edge,poll] [message: Unable to shutdown tunnel] [error: %s]", err)
		}
	}

	if responseData.Status == "REQUIRED" && !operator.tunnelClient.IsTunnelOpen() {
		log.Println("[DEBUG] [http,edge,poll] [message: Required status detected, creating reverse tunnel]")

		err := operator.createTunnel(responseData.Credentials, responseData.Port)
		if err != nil {
			log.Printf("[ERROR] [http,edge,poll] [message: Unable to create tunnel] [error: %s]", err)
			return err
		}
	}

	err = operator.scheduleManager.Schedule(responseData.Schedules)
	if err != nil {
		log.Printf("[ERROR] [http,edge,cron] [message: an error occured during schedule management] [err: %s]", err)
	}

	if responseData.CheckinInterval != operator.pollIntervalInSeconds {
		log.Printf("[DEBUG] [http,edge,poll] [old_interval: %f] [new_interval: %f] [message: updating poll interval]", operator.pollIntervalInSeconds, responseData.CheckinInterval)
		operator.pollIntervalInSeconds = responseData.CheckinInterval
		operator.createHTTPClient(responseData.CheckinInterval)
		go operator.restartStatusPollLoop()
	}

	if responseData.Stacks != nil {
		stacks := map[int]int{}
		for _, stack := range responseData.Stacks {
			stacks[stack.ID] = stack.Version
		}

		err := operator.edgeStackManager.UpdateStacksStatus(stacks)
		if err != nil {
			log.Printf("[ERROR] [http,edge,stacks] [message: an error occured during stack management] [error: %s]", err)
			return err
		}
	}

	return nil
}

func (operator *Operator) createTunnel(encodedCredentials string, remotePort int) error {
	decodedCredentials, err := base64.RawStdEncoding.DecodeString(encodedCredentials)
	if err != nil {
		return err
	}

	credentials, err := libcrypto.Decrypt(decodedCredentials, []byte(operator.edgeID))
	if err != nil {
		return err
	}

	tunnelServerAddr, tunnelServerFingerprint, err := operator.edgeKeyService.GetTunnelConfig()
	if err != nil {
		return err
	}

	tunnelConfig := agent.TunnelConfig{
		ServerAddr:       tunnelServerAddr,
		ServerFingerpint: tunnelServerFingerprint,
		Credentials:      string(credentials),
		RemotePort:       strconv.Itoa(remotePort),
		LocalAddr:        operator.apiServerAddr,
	}

	err = operator.tunnelClient.CreateTunnel(tunnelConfig)
	if err != nil {
		return err
	}

	operator.ResetActivityTimer()
	return nil
}
