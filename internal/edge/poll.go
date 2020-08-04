package edge

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
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/libcrypto"
)

const tunnelActivityCheckInterval = 30 * time.Second

// PollService is used to poll a Portainer instance to retrieve the status associated to the Edge endpoint.
// It is responsible for managing the state of the reverse tunnel (open and closing after inactivity).
// It is also responsible for retrieving the data associated to Edge stacks and schedules.
type PollService struct {
	apiServerAddr           string
	pollIntervalInSeconds   float64
	insecurePoll            bool
	inactivityTimeout       time.Duration
	edgeID                  string
	httpClient              *http.Client
	tunnelClient            agent.ReverseTunnelClient
	scheduleManager         agent.Scheduler
	lastActivity            time.Time
	refreshSignal           chan struct{}
	edgeStackManager        *StackManager
	portainerURL            string
	endpointID              string
	tunnelServerAddr        string
	tunnelServerFingerprint string
	logsManager             *logsManager
	containerPlatform       agent.ContainerPlatform
}

type pollServiceConfig struct {
	APIServerAddr           string
	EdgeID                  string
	InactivityTimeout       string
	PollFrequency           string
	InsecurePoll            bool
	PortainerURL            string
	EndpointID              string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	ContainerPlatform       agent.ContainerPlatform
}

// newPollService returns a pointer to a new instance of PollService
func newPollService(edgeStackManager *StackManager, logsManager *logsManager, config *pollServiceConfig) (*PollService, error) {
	pollFrequency, err := time.ParseDuration(config.PollFrequency)
	if err != nil {
		return nil, err
	}

	inactivityTimeout, err := time.ParseDuration(config.InactivityTimeout)
	if err != nil {
		return nil, err
	}

	return &PollService{
		apiServerAddr:           config.APIServerAddr,
		edgeID:                  config.EdgeID,
		pollIntervalInSeconds:   pollFrequency.Seconds(),
		insecurePoll:            config.InsecurePoll,
		inactivityTimeout:       inactivityTimeout,
		tunnelClient:            chisel.NewClient(),
		scheduleManager:         filesystem.NewCronManager(),
		refreshSignal:           nil,
		edgeStackManager:        edgeStackManager,
		portainerURL:            config.PortainerURL,
		endpointID:              config.EndpointID,
		tunnelServerAddr:        config.TunnelServerAddr,
		tunnelServerFingerprint: config.TunnelServerFingerprint,
		logsManager:             logsManager,
		containerPlatform:       config.ContainerPlatform,
	}, nil
}

func (service *PollService) closeTunnel() error {
	return service.tunnelClient.CloseTunnel()
}

func (service *PollService) resetActivityTimer() {
	if service.tunnelClient.IsTunnelOpen() {
		service.lastActivity = time.Now()
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
	service.startActivityMonitoringLoop()

	return nil
}

func (service *PollService) stop() error {
	if service.refreshSignal != nil {
		close(service.refreshSignal)
		service.refreshSignal = nil
	}
	return nil
}

func (service *PollService) restartStatusPollLoop() {
	service.stop()
	service.refreshSignal = make(chan struct{})
	service.startStatusPollLoop()
}

func (service *PollService) startStatusPollLoop() error {
	log.Printf("[DEBUG] [internal,edge,poll] [poll_interval_seconds: %f] [server_url: %s] [message: starting Portainer short-polling client]", service.pollIntervalInSeconds, service.portainerURL)

	ticker := time.NewTicker(time.Duration(service.pollIntervalInSeconds) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := service.poll()
				if err != nil {
					log.Printf("[ERROR] [internal,edge,poll] [message: an error occured during short poll] [error: %s]", err)
				}

			case <-service.refreshSignal:
				log.Println("[DEBUG] [internal,edge,poll] [message: shutting down Portainer short-polling client]")
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

func (service *PollService) startActivityMonitoringLoop() {
	ticker := time.NewTicker(tunnelActivityCheckInterval)
	quit := make(chan struct{})

	log.Printf("[DEBUG] [internal,edge,monitoring] [monitoring_interval_seconds: %f] [inactivity_timeout: %s] [message: starting activity monitoring loop]", tunnelActivityCheckInterval.Seconds(), service.inactivityTimeout.String())

	go func() {
		for {
			select {
			case <-ticker.C:

				if service.lastActivity.IsZero() {
					continue
				}

				elapsed := time.Since(service.lastActivity)
				log.Printf("[DEBUG] [internal,edge,monitoring] [tunnel_last_activity_seconds: %f] [message: tunnel activity monitoring]", elapsed.Seconds())

				if service.tunnelClient.IsTunnelOpen() && elapsed.Seconds() > service.inactivityTimeout.Seconds() {

					log.Printf("[INFO] [internal,edge,monitoring] [tunnel_last_activity_seconds: %f] [message: shutting down tunnel after inactivity period]", elapsed.Seconds())

					err := service.tunnelClient.CloseTunnel()
					if err != nil {
						log.Printf("[ERROR] [internal,edge,monitoring] [message: unable to shutdown tunnel] [error: %s]", err)
					}
				}

			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

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

func (service *PollService) createHTTPClient(timeout float64) {
	httpCli := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	if service.insecurePoll {
		httpCli.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	service.httpClient = httpCli
}

func (service *PollService) poll() error {

	pollURL := fmt.Sprintf("%s/api/endpoints/%s/status", service.portainerURL, service.endpointID)
	req, err := http.NewRequest("GET", pollURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, service.edgeID)
	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(service.containerPlatform)))

	log.Printf("[DEBUG] [internal,edge,poll] [message: sending agent platform header] [header: %s]", strconv.Itoa(int(service.containerPlatform)))

	if service.httpClient == nil {
		service.createHTTPClient(clientDefaultPollTimeout)
	}

	resp, err := service.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[DEBUG] [internal,edge,poll] [response_code: %d] [message: Poll request failure]", resp.StatusCode)
		return errors.New("short poll request failed")
	}

	var responseData pollStatusResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] [internal,edge,poll] [status: %s] [port: %d] [schedule_count: %d] [checkin_interval_seconds: %f]", responseData.Status, responseData.Port, len(responseData.Schedules), responseData.CheckinInterval)

	if responseData.Status == "IDLE" && service.tunnelClient.IsTunnelOpen() {
		log.Printf("[DEBUG] [internal,edge,poll] [status: %s] [message: Idle status detected, shutting down tunnel]", responseData.Status)

		err := service.tunnelClient.CloseTunnel()
		if err != nil {
			log.Printf("[ERROR] [internal,edge,poll] [message: Unable to shutdown tunnel] [error: %s]", err)
		}
	}

	if responseData.Status == "REQUIRED" && !service.tunnelClient.IsTunnelOpen() {
		log.Println("[DEBUG] [internal,edge,poll] [message: Required status detected, creating reverse tunnel]")

		err := service.createTunnel(responseData.Credentials, responseData.Port)
		if err != nil {
			log.Printf("[ERROR] [internal,edge,poll] [message: Unable to create tunnel] [error: %s]", err)
			return err
		}
	}

	if service.containerPlatform == agent.PlatformDocker {
		err = service.scheduleManager.Schedule(responseData.Schedules)
		if err != nil {
			log.Printf("[ERROR] [internal,edge,cron] [message: an error occured during schedule management] [err: %s]", err)
		}

		logsToCollect := []int{}
		for _, schedule := range responseData.Schedules {
			if schedule.CollectLogs {
				logsToCollect = append(logsToCollect, schedule.ID)
			}
		}

		service.logsManager.handleReceivedLogsRequests(logsToCollect)

		if responseData.CheckinInterval != service.pollIntervalInSeconds {
			log.Printf("[DEBUG] [internal,edge,poll] [old_interval: %f] [new_interval: %f] [message: updating poll interval]", service.pollIntervalInSeconds, responseData.CheckinInterval)
			service.pollIntervalInSeconds = responseData.CheckinInterval
			service.createHTTPClient(responseData.CheckinInterval)
			go service.restartStatusPollLoop()
		}

		if responseData.Stacks != nil {
			stacks := map[int]int{}
			for _, stack := range responseData.Stacks {
				stacks[stack.ID] = stack.Version
			}

			err := service.edgeStackManager.updateStacksStatus(stacks)
			if err != nil {
				log.Printf("[ERROR] [internal,edge,stack] [message: an error occured during stack management] [error: %s]", err)
				return err
			}
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
		ServerAddr:       service.tunnelServerAddr,
		ServerFingerpint: service.tunnelServerFingerprint,
		Credentials:      string(credentials),
		RemotePort:       strconv.Itoa(remotePort),
		LocalAddr:        service.apiServerAddr,
	}

	err = service.tunnelClient.CreateTunnel(tunnelConfig)
	if err != nil {
		return err
	}

	service.resetActivityTimer()
	return nil
}
