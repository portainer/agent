package edge

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/aws"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/filesystem"

	"github.com/rs/zerolog/log"
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
		PollFrequency:           manager.agentOptions.EdgePollFrequency,
		InactivityTimeout:       manager.agentOptions.EdgeInactivityTimeout,
		TunnelCapability:        manager.agentOptions.EdgeTunnel,
		PortainerURL:            manager.key.PortainerInstanceURL,
		TunnelServerAddr:        manager.key.TunnelServerAddr,
		TunnelServerFingerprint: manager.key.TunnelServerFingerprint,
		TunnelProxy:             manager.agentOptions.EdgeTunnelProxy,
		ContainerPlatform:       manager.containerPlatform,
	}

	log.Debug().
		Str("api_addr", pollServiceConfig.APIServerAddr).
		Str("edge_id", pollServiceConfig.EdgeID).
		Str("poll_frequency", pollServiceConfig.PollFrequency).
		Str("inactivity_timeout", pollServiceConfig.InactivityTimeout).
		Bool("insecure_poll", manager.agentOptions.EdgeInsecurePoll).
		Bool("tunnel_capability", manager.agentOptions.EdgeTunnel).
		Msg("")

	// When the header is not set to PlatformDocker Portainer assumes the platform to be kubernetes.
	// However, Portainer should handle podman agents the same way as docker agents.
	agentPlatform := manager.containerPlatform
	if manager.containerPlatform == agent.PlatformPodman {
		agentPlatform = agent.PlatformDocker
	}

	portainerClient := client.NewPortainerClient(
		manager.key.PortainerInstanceURL,
		manager.SetEndpointID,
		manager.GetEndpointID,
		manager.agentOptions.EdgeID,
		manager.agentOptions.EdgeAsyncMode,
		agentPlatform,
		manager.agentOptions.EdgeMetaFields,
		client.BuildHTTPClient(30, manager.agentOptions),
	)

	manager.stackManager = stack.NewStackManager(
		portainerClient,
		manager.agentOptions.AssetsPath,
		aws.ExtractAwsConfig(manager.agentOptions),
		manager.agentOptions.EdgeID,
	)

	manager.logsManager = scheduler.NewLogsManager(portainerClient)
	manager.logsManager.Start()

	pollService, err := newPollService(
		manager,
		manager.stackManager,
		manager.logsManager,
		pollServiceConfig,
		portainerClient,
		manager.agentOptions.EdgeAsyncMode,
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
	if manager.key.EndpointID != endpointID && manager.key.Global {
		log.Info().Int("endpoint_id", int(endpointID)).Msg("setting endpoint ID")

		manager.key.EndpointID = endpointID
	}
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
				log.Error().Msg("an error occurred during Docker runtime configuration check")
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

			err := manager.stackManager.SetEngineType(stack.EngineTypeKubernetes)
			if err != nil {
				log.Error().Err(err).Msg("unable to set engine status")

				return
			}

			err = manager.stackManager.Start()
			if err != nil {
				log.Error().Err(err).Msg("unable to Start stack manager")

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
	runtimeConfig, err := manager.dockerInfoService.GetRuntimeConfigurationFromDockerEngine()
	if err != nil {
		return err
	}

	agentRunsOnLeaderNode := runtimeConfig.DockerConfig.Leader
	agentRunsOnSwarm := runtimeConfig.DockerConfig.EngineType == agent.EngineTypeSwarm

	log.Debug().
		Str("engine_type", fmt.Sprintf("%+v", runtimeConfig.DockerConfig.EngineType)).
		Bool("leader_node", agentRunsOnLeaderNode).
		Msg("Docker runtime configuration check")

	if !agentRunsOnSwarm || agentRunsOnLeaderNode {
		engineType := stack.EngineTypeDockerStandalone
		if agentRunsOnSwarm {
			engineType = stack.EngineTypeDockerSwarm
		}

		manager.pollService.Start()

		err = manager.stackManager.SetEngineType(engineType)
		if err != nil {
			return err
		}

		return manager.stackManager.Start()
	}

	manager.pollService.Stop()
	manager.stackManager.Stop()

	return nil
}

func (manager *Manager) CreateEdgeConfig(config *client.EdgeConfig) error {
	baseDir := filesystem.JoinPaths(agent.HostRoot, config.BaseDir)

	err := filesystem.DecodeDirEntries(config.DirEntries)
	if err != nil {
		return err
	}

	for _, file := range config.DirEntries {
		log.Debug().Str("base", baseDir).Str("path", file.Name).Msg("creating file")
	}

	return filesystem.PersistDir(baseDir, config.DirEntries)
}

func (manager *Manager) DeleteEdgeConfig(config *client.EdgeConfig) error {
	baseDir := filesystem.JoinPaths(agent.HostRoot, config.BaseDir)

	for _, dirEntry := range config.DirEntries {
		path := filesystem.JoinPaths(baseDir, dirEntry.Name)

		if !dirEntry.IsFile {
			continue
		}

		log.Debug().Str("base", baseDir).Str("path", dirEntry.Name).Msg("removing file")

		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Error().Err(err).Str("base", baseDir).Str("path", dirEntry.Name).Msg("failed to remove file")

			return err
		}
	}

	return nil
}

func (manager *Manager) UpdateEdgeConfig(config *client.EdgeConfig) error {
	if err := manager.DeleteEdgeConfig(config.Prev); err != nil {
		return err
	}

	return manager.CreateEdgeConfig(config)
}
