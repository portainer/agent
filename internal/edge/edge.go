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
	key                *edgeKey
	edgeMode           bool
}

// NewEdgeManager creates an instance of EdgeManager
func NewEdgeManager() (*EdgeManager, error) {

	return &EdgeManager{}, nil
}

// Init initializes the manager
func (manager *EdgeManager) Init(options *agent.Options, advertiseAddr string, clusterService agent.ClusterService, infoService agent.InfoService) error {

	apiServerAddr := fmt.Sprintf("%s:%s", advertiseAddr, options.AgentServerPort)

	pollServiceConfig := &pollServiceConfig{
		APIServerAddr:     apiServerAddr,
		EdgeID:            options.EdgeID,
		PollFrequency:     agent.DefaultEdgePollInterval,
		InactivityTimeout: options.EdgeInactivityTimeout,
		InsecurePoll:      options.EdgeInsecurePoll,
	}

	log.Printf("[DEBUG] [internal,edge] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t]", pollServiceConfig.APIServerAddr, pollServiceConfig.EdgeID, pollServiceConfig.PollFrequency, pollServiceConfig.InactivityTimeout, pollServiceConfig.InsecurePoll)

	dockerStackService, err := exec.NewDockerStackService(agent.DockerBinaryPath)
	if err != nil {
		return err
	}
	manager.dockerStackService = dockerStackService

	stacksManager, err := NewStacksManager(dockerStackService, options.EdgeID)
	if err != nil {
		return err
	}
	manager.stacksManager = stacksManager

	pollService, err := newPollService(stacksManager, pollServiceConfig)
	if err != nil {
		return err
	}
	manager.pollService = pollService

	manager.infoService = infoService
	manager.clusterService = clusterService

	edgeKey, err := manager.retrieveEdgeKey(options.EdgeKey)
	if err != nil {
		return err
	}

	if edgeKey != "" {
		log.Println("[DEBUG] [internal,edge] [message: Edge key found in environment. Associating Edge key to cluster.]")

		err := manager.SetKey(edgeKey)
		if err != nil {
			return err
		}
	}

	manager.edgeMode = true

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
		err = manager.pollService.Start(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.key.TunnelServerAddr, manager.key.TunnelServerFingerprint)
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
		err = manager.stacksManager.Start(manager.key.PortainerInstanceURL, manager.key.EndpointID)
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
