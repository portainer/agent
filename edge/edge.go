package edge

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
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
		containerPlatform: parameters.ContainerPlatform,
	}
}

// Start starts the manager
func (manager *Manager) Start() error {
	if !manager.IsKeySet() {
		return errors.New("unable to start Edge manager without key")
	}

	apiServerAddr := fmt.Sprintf("%s:%s", manager.advertiseAddr, manager.agentOptions.AgentServerPort)

	pollServiceConfig := &pollServiceConfig{
		APIServerAddr:           apiServerAddr,
		EdgeID:                  manager.agentOptions.EdgeID,
		PollFrequency:           agent.DefaultEdgePollInterval,
		InactivityTimeout:       manager.agentOptions.EdgeInactivityTimeout,
		InsecurePoll:            manager.agentOptions.EdgeInsecurePoll,
		TunnelCapability:        manager.agentOptions.EdgeTunnel,
		PortainerURL:            manager.key.PortainerInstanceURL,
		EndpointID:              manager.key.EndpointID,
		TunnelServerAddr:        manager.key.TunnelServerAddr,
		TunnelServerFingerprint: manager.key.TunnelServerFingerprint,
		ContainerPlatform:       manager.containerPlatform,
	}

	log.Printf("[DEBUG] [edge] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t] [tunnel_capability: %t]", pollServiceConfig.APIServerAddr, pollServiceConfig.EdgeID, pollServiceConfig.PollFrequency, pollServiceConfig.InactivityTimeout, pollServiceConfig.InsecurePoll, manager.agentOptions.EdgeTunnel)

	stackManager, err := stack.NewStackManager(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID, manager.agentOptions.AssetsPath, pollServiceConfig.InsecurePoll)
	if err != nil {
		return err
	}
	manager.stackManager = stackManager

	manager.logsManager = scheduler.NewLogsManager(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID, pollServiceConfig.InsecurePoll)
	manager.logsManager.Start()

	pollService, err := newPollService(manager.stackManager, manager.logsManager, pollServiceConfig)
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
	manager.pollService.start()

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			manager.pollService.start()

			err := manager.stackManager.SetEngineStatus(stack.EngineTypeKubernetes)
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

		manager.pollService.start()

		err = manager.stackManager.SetEngineStatus(engineStatus)
		if err != nil {
			return err
		}

		return manager.stackManager.Start()
	}

	manager.pollService.stop()

	return manager.stackManager.Stop()
}
