package main

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/edge"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
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

	var edgeManager agent.EdgeManager
	if options.EdgeMode {
		edgeManager, err = edge.NewEdgeManager(options, advertiseAddr, clusterService, infoService)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge] [message: Unable to start edge manger] [error: %s]", err)
		}

		err = edgeManager.Enable(options.EdgeKey)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge] [message: Unable to enable edge mode] [error: %s]", err)
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
		EdgeManager:      edgeManager,
		SignatureService: signatureService,
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

func parseOptions() (*agent.Options, error) {
	optionParser := os.NewEnvOptionParser()
	return optionParser.Options()
}
