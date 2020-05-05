package edge

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http"
	"github.com/portainer/agent/http/client"
	"github.com/portainer/agent/http/edgestacks"
	"github.com/portainer/agent/http/tunnel"
)

type EdgeManager struct {
	clusterService     agent.ClusterService
	dockerStackService agent.DockerStackService
	infoService        agent.InfoService
	stackManager       agent.EdgeStackManager
	tunnelOperator     agent.TunnelOperator
	serverAddr         string
	serverPort         string
	key                *edgeKey
}

func NewEdgeManager(options *agent.Options, advertiseAddr string, clusterService agent.ClusterService, infoService agent.InfoService) (*EdgeManager, error) {
	apiServerAddr := fmt.Sprintf("%s:%s", advertiseAddr, options.AgentServerPort)

	operatorConfig := &tunnel.OperatorConfig{
		APIServerAddr:     apiServerAddr,
		EdgeID:            options.EdgeID,
		PollFrequency:     agent.DefaultEdgePollInterval,
		InactivityTimeout: options.EdgeInactivityTimeout,
		InsecurePoll:      options.EdgeInsecurePoll,
	}

	log.Printf("[DEBUG] [main,edge,configuration] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t]", operatorConfig.APIServerAddr, operatorConfig.EdgeID, operatorConfig.PollFrequency, operatorConfig.InactivityTimeout, operatorConfig.InsecurePoll)

	dockerStackService, err := exec.NewDockerStackService(agent.DockerBinaryPath)
	if err != nil {
		return nil, err
	}

	edgeStackManager, err := edgestacks.NewManager(dockerStackService, options.EdgeID)
	if err != nil {
		return nil, err
	}

	tunnelOperator, err := tunnel.NewTunnelOperator(edgeStackManager, operatorConfig)
	if err != nil {
		return nil, err
	}

	return &EdgeManager{
		clusterService:     clusterService,
		dockerStackService: dockerStackService,
		infoService:        infoService,
		stackManager:       edgeStackManager,
		tunnelOperator:     tunnelOperator,
		serverAddr:         options.EdgeServerAddr,
		serverPort:         options.EdgeServerPort,
	}, nil
}

func (manager *EdgeManager) Enable(edgeKey string) error {
	edgeKey, err := manager.retrieveEdgeKey(edgeKey)
	if err != nil {
		return err
	}

	if edgeKey != "" {
		log.Println("[DEBUG] [main,edge] [message: Edge key found in environment. Associating Edge key to cluster.]")

		err := manager.associateEdgeKey(edgeKey)
		if err != nil {
			return err
		}

	} else {
		log.Println("[DEBUG] [main,edge] [message: Edge key not specified. Serving Edge UI]")

		manager.serveEdgeUI()
	}

	return manager.startRuntimeConfigCheckProcess()

}

func (manager *EdgeManager) ResetActivityTimer() {
	manager.tunnelOperator.ResetActivityTimer()
}

func (manager *EdgeManager) retrieveEdgeKey(edgeKey string) (string, error) {

	if edgeKey != "" {
		log.Println("[INFO] [main,edge] [message: Edge key loaded from options]")
		return edgeKey, nil
	}

	var keyRetrievalError error

	edgeKey, keyRetrievalError = manager.retrieveEdgeKeyFromFilesystem()
	if keyRetrievalError != nil {
		return "", keyRetrievalError
	}

	if edgeKey == "" && manager.clusterService != nil {
		edgeKey, keyRetrievalError = retrieveEdgeKeyFromCluster(manager.clusterService)
		if keyRetrievalError != nil {
			return "", keyRetrievalError
		}
	}

	return edgeKey, nil
}

func (manager *EdgeManager) retrieveEdgeKeyFromFilesystem() (string, error) {
	var edgeKey string

	edgeKeyFilePath := fmt.Sprintf("%s/%s", agent.DataDirectory, agent.EdgeKeyFile)

	keyFileExists, err := filesystem.FileExists(edgeKeyFilePath)
	if err != nil {
		return "", err
	}

	if keyFileExists {
		filesystemKey, err := filesystem.ReadFromFile(edgeKeyFilePath)
		if err != nil {
			return "", err
		}

		log.Println("[INFO] [main,edge] [message: Edge key loaded from the filesystem]")
		edgeKey = string(filesystemKey)
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromCluster(clusterService agent.ClusterService) (string, error) {
	var edgeKey string

	member := clusterService.GetMemberWithEdgeKeySet()
	if member != nil {
		httpCli := client.NewAPIClient()

		memberAddr := fmt.Sprintf("%s:%s", member.IPAddress, member.Port)
		memberKey, err := httpCli.GetEdgeKey(memberAddr)
		if err != nil {
			log.Printf("[ERROR] [main,edge,http,cluster] [message: Unable to retrieve Edge key from cluster member] [error: %s]", err)
			return "", err
		}

		log.Println("[INFO] [main,edge] [message: Edge key loaded from cluster]")
		edgeKey = memberKey
	}

	return edgeKey, nil
}

func (manager *EdgeManager) associateEdgeKey(edgeKey string) error {
	err := manager.SetKey(edgeKey)
	if err != nil {
		return err
	}

	if manager.clusterService != nil {
		tags := manager.clusterService.GetTags()
		tags[agent.MemberTagEdgeKeySet] = "set"
		err = manager.clusterService.UpdateTags(tags)
		if err != nil {
			return err
		}
	}

	return nil
}

func (manager *EdgeManager) serveEdgeUI() {
	edgeServer := http.NewEdgeServer(manager, manager.clusterService)

	go func() {
		log.Printf("[INFO] [main,edge,http] [server_address: %s] [server_port: %s] [message: Starting Edge server]", manager.serverAddr, manager.serverPort)

		err := edgeServer.Start(manager.serverAddr, manager.serverPort)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,http] [message: Unable to start Edge server] [error: %s]", err)
		}

		log.Println("[INFO] [main,edge,http] [message: Edge server shutdown]")
	}()

	go func() {
		timer1 := time.NewTimer(agent.DefaultEdgeSecurityShutdown * time.Minute)
		<-timer1.C

		if !manager.IsKeySet() {
			log.Printf("[INFO] [main,edge,http] [message: Shutting down Edge UI server as no key was specified after %d minutes]", agent.DefaultEdgeSecurityShutdown)
			edgeServer.Shutdown()
		}
	}()
}

func (manager *EdgeManager) startRuntimeConfigCheckProcess() error {

	runtimeCheckFrequency, err := time.ParseDuration(agent.DefaultConfigCheckInterval)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(runtimeCheckFrequency)

	go func() {
		for {
			select {
			case <-ticker.C:
				key := manager.GetKey()
				if key == "" {
					continue
				}

				agentTags, err := manager.infoService.GetInformationFromDockerEngine()
				if err != nil {
					log.Printf("[ERROR] [main,edge,docker] [message: an error occured during Docker runtime configuration check] [error: %s]", err)
					continue
				}

				isLeader := agentTags[agent.MemberTagKeyIsLeader] == "1"
				isSwarm := agentTags[agent.MemberTagEngineStatus] == agent.EngineStatusSwarm

				log.Printf("[DEBUG] [main,edge,docker] [message: Docker runtime configuration check] [engine_status: %s] [leader_node: %t]", agentTags[agent.MemberTagEngineStatus], isLeader)

				if !isSwarm || isLeader {
					err = manager.tunnelOperator.Start(manager.key.PortainerInstanceURL, manager.key.EndpointID, manager.key.TunnelServerAddr, manager.key.TunnelServerFingerprint)
					if err != nil {
						log.Printf("[ERROR] [main,edge,docker] [message: an error occured while starting poll] [error: %s]", err)
					}

				} else {
					err = manager.tunnelOperator.Stop()
					if err != nil {
						log.Printf("[ERROR] [main,edge,docker] [message: an error occured while stopping the short-poll process] [error: %s]", err)
					}

				}

				if isSwarm && isLeader {
					err = manager.stackManager.Start(manager.key.PortainerInstanceURL, manager.key.EndpointID)
					if err != nil {
						log.Printf("[ERROR] [main,edge,stack] [message: an error occured while starting the Edge stack manager] [error: %s]", err)
					}

				} else {
					err = manager.stackManager.Stop()
					if err != nil {
						log.Printf("[ERROR] [main,edge,stack] [message: an error occured while stopping the Edge stack manager] [error: %s]", err)
					}

				}
			}
		}
	}()

	return nil
}
