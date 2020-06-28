package edge

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/exec"
)

// Manager is used to manage all Edge features through multiple sub-components. It is mainly responsible for running the Edge background process.
type Manager struct {
	advertiseAddr      string
	agentOptions       *agent.Options
	clusterService     agent.ClusterService
	dockerStackService agent.DockerStackService
	edgeMode           bool
	infoService        agent.InfoService
	key                *edgeKey
	logsManager        *logsManager
	pollService        *PollService
	pollServiceConfig  *pollServiceConfig
	stackManager       *StackManager
}

// NewManager returns a pointer to a new instance of Manager
func NewManager(options *agent.Options, advertiseAddr string, clusterService agent.ClusterService, infoService agent.InfoService) *Manager {
	return &Manager{
		clusterService: clusterService,
		infoService:    infoService,
		agentOptions:   options,
		advertiseAddr:  advertiseAddr,
		edgeMode:       options.EdgeMode,
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
	}

	log.Printf("[DEBUG] [internal,edge] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t]", pollServiceConfig.APIServerAddr, pollServiceConfig.EdgeID, pollServiceConfig.PollFrequency, pollServiceConfig.InactivityTimeout, pollServiceConfig.InsecurePoll)

	dockerStackService, err := exec.NewDockerStackService(agent.DockerBinaryPath)
	if err != nil {
		return err
	}
	manager.dockerStackService = dockerStackService

	manager.stackManager = newStackManager(dockerStackService, manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID)

	manager.logsManager = newLogsManager(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID)
	manager.logsManager.start()

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

func (manager *Manager) startEdgeBackgroundProcess() error {
	runtimeCheckFrequency, err := time.ParseDuration(agent.DefaultConfigCheckInterval)
	if err != nil {
		return err
	}

	err = manager.checkRuntimeConfig()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(runtimeCheckFrequency)

	go func() {
		for {
			select {
			case <-ticker.C:
				err := manager.checkRuntimeConfig()
				if err != nil {
					log.Printf("[ERROR] [internal,edge,runtime] [message: an error occured during Docker runtime configuration check] [error: %s]", err)
				}
			}
		}
	}()

	return nil
}

func (manager *Manager) checkRuntimeConfig() error {
	agentTags, err := manager.infoService.GetInformationFromDockerEngine()
	if err != nil {
		return err
	}

	agentRunsOnLeaderNode := agentTags.Leader
	agentRunsOnSwarm := agentTags.EngineStatus == agent.EngineStatusSwarm

	log.Printf("[DEBUG] [internal,edge,docker] [message: Docker runtime configuration check] [engine_status: %d] [leader_node: %t]", agentTags.EngineStatus, agentRunsOnLeaderNode)

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
