package edge

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	portainer "github.com/portainer/portainer/api"
)

type (
	// Manager is used to manage all Edge features through multiple sub-components. It is mainly responsible for running the Edge background process.
	Manager struct {
		containerPlatform agent.ContainerPlatform
		advertiseAddr     string
		agentOptions      *agent.Options
		clusterService    agent.ClusterService
		dockerInfoService agent.DockerInfoService
		key               *edgeKey
		logsManager       *scheduler.LogsManager
		pollService       *PollService
		stackManager      *stack.StackManager
		mu                sync.Mutex
	}

	// ManagerParameters represents an object used to create a Manager
	ManagerParameters struct {
		Options           *agent.Options
		AdvertiseAddr     string
		ClusterService    agent.ClusterService
		DockerInfoService agent.DockerInfoService
		ContainerPlatform agent.ContainerPlatform
	}
)

func (manager *Manager) GetStackManager() *stack.StackManager {
	return manager.stackManager
}

// NewManager returns a pointer to a new instance of Manager
func NewManager(parameters *ManagerParameters) *Manager {
	return &Manager{
		clusterService:    parameters.ClusterService,
		dockerInfoService: parameters.DockerInfoService,
		agentOptions:      parameters.Options,
		advertiseAddr:     parameters.AdvertiseAddr,
		containerPlatform: parameters.ContainerPlatform,
	}
}

// Start starts the manager
func (manager *Manager) Start() error {
	if !manager.IsKeySet() {
		return errors.New("unable to Start Edge manager without key")
	}

	apiServerAddr := fmt.Sprintf("%s:%s", manager.advertiseAddr, manager.agentOptions.AgentServerPort)

	pollServiceConfig := &pollServiceConfig{
		APIServerAddr:           apiServerAddr,
		EdgeID:                  manager.agentOptions.EdgeID,
		PollFrequency:           agent.DefaultEdgePollInterval,
		InactivityTimeout:       manager.agentOptions.EdgeInactivityTimeout,
		TunnelCapability:        manager.agentOptions.EdgeTunnel,
		PortainerURL:            manager.key.PortainerInstanceURL,
		TunnelServerAddr:        manager.key.TunnelServerAddr,
		TunnelServerFingerprint: manager.key.TunnelServerFingerprint,
		ContainerPlatform:       manager.containerPlatform,
	}

	log.Printf("[DEBUG] [edge] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t] [tunnel_capability: %t]", pollServiceConfig.APIServerAddr, pollServiceConfig.EdgeID, pollServiceConfig.PollFrequency, pollServiceConfig.InactivityTimeout, manager.agentOptions.EdgeInsecurePoll, manager.agentOptions.EdgeTunnel)

	// When the header is not set to PlatformDocker Portainer assumes the platform to be kubernetes.
	// However, Portainer should handle podman agents the same way as docker agents.
	agentPlatform := manager.containerPlatform
	if manager.containerPlatform == agent.PlatformPodman {
		agentPlatform = agent.PlatformDocker
	}

	portainerClient := client.NewPortainerClient(
		manager.key.PortainerInstanceURL,
		manager.GetEndpointID,
		manager.agentOptions.EdgeID,
		manager.agentOptions.EdgeAsyncMode,
		agentPlatform,
		buildHTTPClient(10, manager.agentOptions),
	)

	manager.stackManager = stack.NewStackManager(
		portainerClient,
		manager.agentOptions.AssetsPath,
	)

	manager.logsManager = scheduler.NewLogsManager(portainerClient)
	manager.logsManager.Start()

	pollService, err := newPollService(
		manager,
		manager.stackManager,
		manager.logsManager,
		pollServiceConfig,
		portainerClient,
	)
	if err != nil {
		return err
	}
	manager.pollService = pollService

	return manager.startEdgeBackgroundProcess()
}

// ResetActivityTimer resets the activity timer
func (manager *Manager) ResetActivityTimer() {
	manager.pollService.resetActivityTimer()
}

// SetEndpointID set the endpointID of the agent
func (manager *Manager) SetEndpointID(endpointID portainer.EndpointID) {
	manager.mu.Lock()
	manager.key.EndpointID = endpointID
	manager.mu.Unlock()
}

// GetEndpointID gets the endpointID of the agent
func (manager *Manager) GetEndpointID() portainer.EndpointID {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	return manager.key.EndpointID
}

func (manager *Manager) startEdgeBackgroundProcessOnDocker(runtimeCheckFrequency time.Duration) error {
	err := manager.checkDockerRuntimeConfig()
	if err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			err := manager.checkDockerRuntimeConfig()
			if err != nil {
				log.Printf("[ERROR] [edge] [message: an error occurred during Docker runtime configuration check] [error: %s]", err)
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcessOnKubernetes(runtimeCheckFrequency time.Duration) error {
	manager.pollService.Start()

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			manager.pollService.Start()

			err := manager.stackManager.SetEngineStatus(stack.EngineTypeKubernetes)
			if err != nil {
				log.Printf("[ERROR] [internal,edge,runtime] [message: unable to set engine status] [error: %s]", err)
				return
			}

			err = manager.stackManager.Start()
			if err != nil {
				log.Printf("[ERROR] [internal,edge,runtime] [message: unable to Start stack manager] [error: %s]", err)
				return
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcessOnNomad(runtimeCheckFrequency time.Duration) error {
	manager.pollService.Start()

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			manager.pollService.Start()

			err := manager.stackManager.SetEngineStatus(stack.EngineTypeNomad)
			if err != nil {
				log.Printf("[ERROR] [internal,edge,runtime] [message: unable to set engine status] [error: %s]", err)
				return
			}

			err = manager.stackManager.Start()
			if err != nil {
				log.Printf("[ERROR] [internal,edge,runtime] [message: unable to start stack manager] [error: %s]", err)
				return
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcess() error {
	runtimeCheckFrequency, err := time.ParseDuration(agent.DefaultConfigCheckInterval)
	if err != nil {
		return err
	}

	switch manager.containerPlatform {
	case agent.PlatformDocker:
		return manager.startEdgeBackgroundProcessOnDocker(runtimeCheckFrequency)
	case agent.PlatformKubernetes:
		return manager.startEdgeBackgroundProcessOnKubernetes(runtimeCheckFrequency)
	case agent.PlatformNomad:
		return manager.startEdgeBackgroundProcessOnNomad(runtimeCheckFrequency)
	}

	return nil
}

func (manager *Manager) checkDockerRuntimeConfig() error {
	runtimeConfiguration, err := manager.dockerInfoService.GetRuntimeConfigurationFromDockerEngine()
	if err != nil {
		return err
	}

	agentRunsOnLeaderNode := runtimeConfiguration.DockerConfiguration.Leader
	agentRunsOnSwarm := runtimeConfiguration.DockerConfiguration.EngineStatus == agent.EngineStatusSwarm

	log.Printf("[DEBUG] [edge] [message: Docker runtime configuration check] [engine_status: %d] [leader_node: %t]", runtimeConfiguration.DockerConfiguration.EngineStatus, agentRunsOnLeaderNode)

	if !agentRunsOnSwarm || agentRunsOnLeaderNode {
		engineStatus := stack.EngineTypeDockerStandalone
		if agentRunsOnSwarm {
			engineStatus = stack.EngineTypeDockerSwarm
		}

		manager.pollService.Start()

		err = manager.stackManager.SetEngineStatus(engineStatus)
		if err != nil {
			return err
		}

		return manager.stackManager.Start()
	}

	manager.pollService.Stop()

	return manager.stackManager.Stop()
}

func buildHTTPClient(timeout float64, options *agent.Options) *http.Client {
	httpCli := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	transport := buildTransport(options)
	if transport != nil {
		httpCli.Transport = transport
	}
	return httpCli
}

func buildTransport(options *agent.Options) *http.Transport {
	if options.EdgeInsecurePoll {
		return &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	// if ssl certs for edge agent are set
	if options.SSLCert != "" && options.SSLKey != "" {

		var caCertPool *x509.CertPool
		// Create a CA certificate pool and add cert.pem to it
		if options.SSLCACert != "" {
			caCert, err := ioutil.ReadFile(options.SSLCACert)
			if err != nil {
				log.Fatal(err)
			}
			caCertPool = x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
		}

		// Create an HTTPS client and supply the created CA pool and certificate
		return &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
				GetClientCertificate: func(chi *tls.CertificateRequestInfo) (*tls.Certificate, error) {
					cert, err := tls.LoadX509KeyPair(options.SSLCert, options.SSLKey)
					if err != nil {
						return nil, err
					}

					return &cert, nil
				},
			},
		}
	}

	return nil
}
