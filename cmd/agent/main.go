package main

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
	"github.com/portainer/agent/http/client"
	"github.com/portainer/agent/http/tunnel"
	"github.com/portainer/agent/logutils"
	"github.com/portainer/agent/net"
	"github.com/portainer/agent/os"
	cluster "github.com/portainer/agent/serf"
)

func main() {
	options, err := parseOptions()
	if err != nil {
		log.Fatalf("[ERROR] [main,configuration] [message: Invalid agent configuration] [error: %s]", err)
	}

	logutils.SetupLogger(options.LogLevel)

	var infoService agent.InfoService = docker.NewInfoService()

	agentTags, err := infoService.GetInformationFromDockerEngine()
	if err != nil {
		log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve information from Docker] [error: %s]", err)
	}

	agentTags[agent.MemberTagKeyAgentPort] = options.AgentServerPort
	log.Printf("[DEBUG] [main,configuration] [Member tags: %+v]", agentTags)

	clusterMode := false
	if agentTags[agent.MemberTagEngineStatus] == agent.EngineStatusSwarm {
		clusterMode = true
		log.Println("[INFO] [main] [message: Agent running on a Swarm cluster node. Running in cluster mode]")
	}

	containerName, err := os.GetHostName()
	if err != nil {
		log.Fatalf("[ERROR] [main,os] [message: Unable to retrieve container name] [error: %s]", err)
	}

	advertiseAddr, err := infoService.GetContainerIpFromDockerEngine(containerName, clusterMode)
	if err != nil {
		log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve local agent IP address] [error: %s]", err)
	}

	var clusterService agent.ClusterService
	if clusterMode {
		clusterService = cluster.NewClusterService(agentTags)

		clusterAddr := options.ClusterAddress
		if clusterAddr == "" {
			serviceName, err := infoService.GetServiceNameFromDockerEngine(containerName)
			if err != nil {
				log.Fatalf("[ERROR] [main,docker] [message: Unable to agent service name from Docker] [error: %s]", err)
			}

			clusterAddr = fmt.Sprintf("tasks.%s", serviceName)
		}

		// TODO: Workaround. looks like the Docker DNS cannot find any info on tasks.<service_name>
		// sometimes... Waiting a bit before starting the discovery (at least 3 seconds) seems to solve the problem.
		time.Sleep(3 * time.Second)

		joinAddr, err := net.LookupIPAddresses(clusterAddr)
		if err != nil {
			log.Fatalf("[ERROR] [main,net] [host: %s] [message: Unable to retrieve a list of IP associated to the host] [error: %s]", clusterAddr, err)
		}

		err = clusterService.Create(advertiseAddr, joinAddr)
		if err != nil {
			log.Fatalf("[ERROR] [main,cluster] [message: Unable to create cluster] [error: %s]", err)
		}

		log.Printf("[DEBUG] [main,configuration] [agent_port: %s] [cluster_address: %s] [advertise_address: %s]", options.AgentServerPort, clusterAddr, advertiseAddr)

		defer clusterService.Leave()
	}

	var tunnelOperator agent.TunnelOperator
	if options.EdgeMode {
		apiServerAddr := fmt.Sprintf("%s:%s", advertiseAddr, options.AgentServerPort)

		operatorConfig := &tunnel.OperatorConfig{
			APIServerAddr:     apiServerAddr,
			EdgeID:            options.EdgeID,
			PollFrequency:     agent.DefaultEdgePollInterval,
			InactivityTimeout: options.EdgeInactivityTimeout,
			InsecurePoll:      options.EdgeInsecurePoll,
		}

		log.Printf("[DEBUG] [main,edge,configuration] [api_addr: %s] [edge_id: %s] [poll_frequency: %s] [inactivity_timeout: %s] [insecure_poll: %t]", operatorConfig.APIServerAddr, operatorConfig.EdgeID, operatorConfig.PollFrequency, operatorConfig.InactivityTimeout, operatorConfig.InsecurePoll)

		tunnelOperator, err = tunnel.NewTunnelOperator(operatorConfig)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,rtunnel] [message: Unable to create tunnel operator] [error: %s]", err)
		}

		err := enableEdgeMode(tunnelOperator, clusterService, infoService, options)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,rtunnel] [message: Unable to start agent in Edge mode] [error: %s]", err)
		}

		dockerStackService, err := exec.NewDockerStackService(agent.DockerBinaryPath)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,docker] [message: Unable to start docker stack service] [error: %s]", err)
		}
	}

	systemService := ghw.NewSystemService(agent.HostRoot)

	var signatureService agent.DigitalSignatureService
	if !options.EdgeMode {
		signatureService = crypto.NewECDSAService(options.SharedSecret)
		tlsService := crypto.TLSService{}

		err := tlsService.GenerateCertsForHost(advertiseAddr)
		if err != nil {
			log.Fatalf("[ERROR] [main,tls] [message: Unable to generate self-signed certificates] [error: %s]", err)
		}
	}

	config := &http.APIServerConfig{
		Addr:             options.AgentServerAddr,
		Port:             options.AgentServerPort,
		SystemService:    systemService,
		ClusterService:   clusterService,
		SignatureService: signatureService,
		TunnelOperator:   tunnelOperator,
		AgentTags:        agentTags,
		AgentOptions:     options,
		EdgeMode:         options.EdgeMode,
	}

	if options.EdgeMode {
		config.Addr = advertiseAddr
	}

	err = startAPIServer(config)
	if err != nil {
		log.Fatalf("[ERROR] [main,http] [message: Unable to start Agent API server] [error: %s]", err)
	}
}

func startAPIServer(config *http.APIServerConfig) error {
	server := http.NewAPIServer(config)

	if config.EdgeMode {
		return server.StartUnsecured()
	}

	return server.StartSecured()
}

func enableEdgeMode(tunnelOperator agent.TunnelOperator, clusterService agent.ClusterService, infoService agent.InfoService, options *agent.Options) error {
	edgeKey, err := retrieveEdgeKey(options, clusterService)
	if err != nil {
		return err
	}

	if edgeKey != "" {
		log.Println("[DEBUG] [main,edge] [message: Edge key found in environment. Associating Edge key to cluster.]")

		err := associateEdgeKey(tunnelOperator, clusterService, edgeKey)
		if err != nil {
			return err
		}

	} else {
		log.Println("[DEBUG] [main,edge] [message: Edge key not specified. Serving Edge UI]")

		serveEdgeUI(tunnelOperator, clusterService, options)
	}

	return startRuntimeConfigCheckProcess(tunnelOperator, infoService)
}

func retrieveEdgeKey(options *agent.Options, clusterService agent.ClusterService) (string, error) {
	edgeKey := options.EdgeKey

	if options.EdgeKey != "" {
		log.Println("[INFO] [main,edge] [message: Edge key loaded from options]")
		edgeKey = options.EdgeKey
	}

	if edgeKey == "" {
		var keyRetrievalError error

		edgeKey, keyRetrievalError = retrieveEdgeKeyFromFilesystem()
		if keyRetrievalError != nil {
			return "", keyRetrievalError
		}

		if edgeKey == "" && clusterService != nil {
			edgeKey, keyRetrievalError = retrieveEdgeKeyFromCluster(clusterService)
			if keyRetrievalError != nil {
				return "", keyRetrievalError
			}
		}
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromFilesystem() (string, error) {
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

func parseOptions() (*agent.Options, error) {
	optionParser := os.NewEnvOptionParser()
	return optionParser.Options()
}

func associateEdgeKey(tunnelOperator agent.TunnelOperator, clusterService agent.ClusterService, edgeKey string) error {
	err := tunnelOperator.SetKey(edgeKey)
	if err != nil {
		return err
	}

	if clusterService != nil {
		tags := clusterService.GetTags()
		tags[agent.MemberTagEdgeKeySet] = "set"
		err = clusterService.UpdateTags(tags)
		if err != nil {
			return err
		}
	}

	return nil
}

func serveEdgeUI(tunnelOperator agent.TunnelOperator, clusterService agent.ClusterService, options *agent.Options) {
	edgeServer := http.NewEdgeServer(tunnelOperator, clusterService)

	go func() {
		log.Printf("[INFO] [main,edge,http] [server_address: %s] [server_port: %s] [message: Starting Edge server]", options.EdgeServerAddr, options.EdgeServerPort)

		err := edgeServer.Start(options.EdgeServerAddr, options.EdgeServerPort)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,http] [message: Unable to start Edge server] [error: %s]", err)
		}

		log.Println("[INFO] [main,edge,http] [message: Edge server shutdown]")
	}()

	go func() {
		timer1 := time.NewTimer(agent.DefaultEdgeSecurityShutdown * time.Minute)
		<-timer1.C

		if !tunnelOperator.IsKeySet() {
			log.Printf("[INFO] [main,edge,http] [message: Shutting down Edge UI server as no key was specified after %d minutes]", agent.DefaultEdgeSecurityShutdown)
			edgeServer.Shutdown()
		}
	}()
}

func startRuntimeConfigCheckProcess(tunnelOperator agent.TunnelOperator, infoService agent.InfoService) error {

	runtimeCheckFrequency, err := time.ParseDuration(agent.DefaultConfigCheckInterval)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(runtimeCheckFrequency)

	go func() {
		for {
			select {
			case <-ticker.C:
				key := tunnelOperator.GetKey()
				if key == "" {
					continue
				}

				agentTags, err := infoService.GetInformationFromDockerEngine()
				if err != nil {
					log.Printf("[ERROR] [main,edge,docker] [message: an error occured during Docker runtime configuration check] [error: %s]", err)
					continue
				}

				log.Printf("[DEBUG] [main,edge,docker] [message: Docker runtime configuration check] [engine_status: %s] [leader_node: %t]", agentTags[agent.MemberTagEngineStatus], agentTags[agent.MemberTagKeyIsLeader] == "1")

				if agentTags[agent.MemberTagEngineStatus] == agent.EngineStatusStandalone || agentTags[agent.MemberTagKeyIsLeader] == "1" {
					err = tunnelOperator.Start()
					if err != nil {
						log.Printf("[ERROR] [main,edge,docker] [message: an error occured while starting poll] [error: %s]", err)
					}

				} else {
					err = tunnelOperator.Stop()
					if err != nil {
						log.Printf("[ERROR] [main,edge,docker] [message: an error occured while stopping the short-poll process] [error: %s]", err)
					}
				}
			}
		}
	}()

	return nil
}
