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
	clusterService     agent.ClusterService
	dockerStackService agent.DockerStackService
	infoService        agent.InfoService
	stacksManager      *StacksManager
	pollService        *PollService
	pollServiceConfig  *pollServiceConfig
	key                *edgeKey
	edgeMode           bool
	agentOptions       *agent.Options
	advertiseAddr      string
}

// NewManager returns a pointer to a new instance of Manager
func NewManager(options *agent.Options, advertiseAddr string, clusterService agent.ClusterService, infoService agent.InfoService) (*Manager, error) {

	return &Manager{
		clusterService: clusterService,
		infoService:    infoService,
		agentOptions:   options,
		advertiseAddr:  advertiseAddr,
		edgeMode:       options.EdgeMode,
	}, nil
}

// Init initializes the manager
func (manager *Manager) Init() error {
	if !manager.IsKeySet() {
		return errors.New("Unable to initialize Edge manager without key")
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

	stacksManager, err := NewStacksManager(dockerStackService, manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.agentOptions.EdgeID)
	if err != nil {
		return err
	}
	manager.stacksManager = stacksManager

	pollService, err := newPollService(stacksManager, pollServiceConfig)
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

	agentRunsOnLeaderNode := agentTags[agent.MemberTagKeyIsLeader] == "1"
	agentRunsOnSwarm := agentTags[agent.MemberTagEngineStatus] == agent.EngineStatusSwarm

	log.Printf("[DEBUG] [internal,edge,docker] [message: Docker runtime configuration check] [engine_status: %s] [leader_node: %t]", agentTags[agent.MemberTagEngineStatus], agentRunsOnLeaderNode)

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
		err = manager.stacksManager.Start()
		if err != nil {
			return err
		}

	} else {
		err = manager.stacksManager.Stop()
		if err != nil {
			return err
		}
	}

	return nil
}
