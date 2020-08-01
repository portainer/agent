package edge

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
)

type (
	// Manager is used to manage all Edge features through multiple sub-components. It is mainly responsible for running the Edge background process.
	Manager struct {
		containerPlatform  agent.ContainerPlatform
		advertiseAddr      string
		agentOptions       *agent.Options
		clusterService     agent.ClusterService
		dockerStackService agent.DockerStackService
		edgeMode           bool
		dockerInfoService  agent.DockerInfoService
		key                *edgeKey
		logsManager        *logsManager
		pollService        *PollService
		pollServiceConfig  *pollServiceConfig
		stackManager       *StackManager
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

// NewManager returns a pointer to a new instance of Manager
func NewManager(parameters *ManagerParameters) *Manager {
	return &Manager{
		clusterService:    parameters.ClusterService,
		dockerInfoService: parameters.DockerInfoService,
		agentOptions:      parameters.Options,
		advertiseAddr:     parameters.AdvertiseAddr,
		edgeMode:          parameters.Options.EdgeMode,
		containerPlatform: parameters.ContainerPlatform,
	}
}

// Start starts the manager
func (manager *Manager) Start() error {
	if !manager.IsKeySet() {
		return errors.New("Unable to start Edge manager without key")
	}

	apiServerAddr := fmt.Sprintf("%s:%s", manager.advertiseAddr, manager.agentOptions.AgentServerPort)

	pollServiceConfig := &pollServiceConfig{
		APIServerAddr:           apiServerAddr,
		EdgeID:                  manager.agentOptions.EdgeID,
		PollFrequency:           agent.DefaultEdgePollInterval,
		InactivityTimeout:       manager.agentOptions.EdgeInactivityTimeout,
		InsecurePoll:            manager.agentOptions.EdgeInsecurePoll,
		PortainerURL:            manager.key.PortainerInstanceURL,
		EndpointID:              manager.key.EndpointID,
		TunnelServerAddr:        manager.key.TunnelServerAddr,
		TunnelServerFingerprint: manager.key.TunnelServerFingerprint,
		ContainerPlatform:       manager.containerPlatform,
	}

	log.Printf("[DEBUG] [internal,edge] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t]", pollServiceConfig.APIServerAddr, pollServiceConfig.EdgeID, pollServiceConfig.PollFrequency, pollServiceConfig.InactivityTimeout, pollServiceConfig.InsecurePoll)

	if manager.containerPlatform == agent.PlatformDocker {
		stackManager, err := newStackManager(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID)
		if err != nil {
			return err
		}
		manager.stackManager = stackManager

		manager.logsManager = newLogsManager(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID)
		manager.logsManager.start()
	}

	pollService, err := newPollService(manager.stackManager, manager.logsManager, pollServiceConfig)
	if err != nil {
		return err
	}
	manager.pollService = pollService

	err = manager.startEdgeBackgroundProcess()
	if err != nil {
		return err
	}

	return nil
}

// IsEdgeModeEnabled returns true if edge mode is enabled
func (manager *Manager) IsEdgeModeEnabled() bool {
	return manager.edgeMode
}

// ResetActivityTimer resets the activity timer
func (manager *Manager) ResetActivityTimer() {
	manager.pollService.resetActivityTimer()
}

func (manager *Manager) startEdgeBackgroundProcessOnDocker(runtimeCheckFrequency time.Duration) error {
	err := manager.checkDockerRuntimeConfig()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(runtimeCheckFrequency)

	go func() {
		for {
			select {
			case <-ticker.C:
				err := manager.checkDockerRuntimeConfig()
				if err != nil {
					log.Printf("[ERROR] [internal,edge,runtime,docker] [message: an error occured during Docker runtime configuration check] [error: %s]", err)
				}
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcessOnKubernetes(runtimeCheckFrequency time.Duration) error {
	err := manager.pollService.start()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(runtimeCheckFrequency)

	go func() {
		for {
			select {
			case <-ticker.C:
				err := manager.pollService.start()
				if err != nil {
					log.Printf("[ERROR] [internal,edge,runtime] [message: unable to start short-poll service] [error: %s]", err)
				}

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

	log.Printf("[DEBUG] [internal,edge,runtime,docker] [message: Docker runtime configuration check] [engine_status: %d] [leader_node: %t]", runtimeConfiguration.DockerConfiguration.EngineStatus, agentRunsOnLeaderNode)

	if !agentRunsOnSwarm || agentRunsOnLeaderNode {
		err = manager.pollService.start()
		if err != nil {
			return err
		}

	} else {
		err = manager.pollService.stop()
		if err != nil {
			return err
		}
	}

	if agentRunsOnSwarm && agentRunsOnLeaderNode {
		err = manager.stackManager.start()
		if err != nil {
			return err
		}
	} else {
		err = manager.stackManager.stop()
		if err != nil {
			return err
		}
	}

	return nil
}
