package edge

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/exec"
)

// EdgeManager manages Edge functionality
type EdgeManager struct {
	clusterService     agent.ClusterService
	dockerStackService agent.DockerStackService
	infoService        agent.InfoService
	stacksManager      *StacksManager
	pollService        agent.TunnelOperator
	pollServiceConfig  *pollServiceConfig
	key                *edgeKey
	edgeMode           bool
	agentOptions       *agent.Options
	advertiseAddr      string
}

// NewEdgeManager creates an instance of EdgeManager
func NewEdgeManager(options *agent.Options, advertiseAddr string, clusterService agent.ClusterService, infoService agent.InfoService) (*EdgeManager, error) {

	return &EdgeManager{
		clusterService: clusterService,
		infoService:    infoService,
		agentOptions:   options,
		advertiseAddr:  advertiseAddr,
		edgeMode:       options.EdgeMode,
	}, nil
}

// Init initializes the manager
func (manager *EdgeManager) Init() error {
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
func (manager *EdgeManager) IsEdgeModeEnabled() bool {
	return manager.edgeMode
}

// ResetActivityTimer resets the activity timer
func (manager *EdgeManager) ResetActivityTimer() {
	manager.pollService.ResetActivityTimer()
}

func (manager *EdgeManager) startEdgeBackgroundProcess() error {

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

func (manager *EdgeManager) checkRuntimeConfig() error {
	agentTags, err := manager.infoService.GetInformationFromDockerEngine()
	if err != nil {
		return err
	}

	agentRunsOnLeaderNode := agentTags[agent.MemberTagKeyIsLeader] == "1"
	agentRunsOnSwarm := agentTags[agent.MemberTagEngineStatus] == agent.EngineStatusSwarm

	log.Printf("[DEBUG] [internal,edge,docker] [message: Docker runtime configuration check] [engine_status: %s] [leader_node: %t]", agentTags[agent.MemberTagEngineStatus], agentRunsOnLeaderNode)

	if !agentRunsOnSwarm || agentRunsOnLeaderNode {
		err = manager.pollService.Start()
		if err != nil {
			return err
		}

	} else {
		err = manager.pollService.Stop()
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
