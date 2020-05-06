package main

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
	"github.com/portainer/agent/internal/edge"
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

	edgeManager, err := edge.NewEdgeManager()
	if err != nil {
		log.Fatalf("[ERROR] [main,edge] [message: Unable to start edge manger] [error: %s]", err)
	}

	if options.EdgeMode {
		err = edgeManager.Init(options, advertiseAddr, clusterService, infoService)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge] [message: Unable to init edge manager] [error: %s]", err)
		}

		if !edgeManager.IsKeySet() {
			log.Println("[DEBUG] [main,edge] [message: Edge key not specified. Serving Edge UI]")

			serveEdgeUI(edgeManager, clusterService, options.EdgeServerAddr, options.EdgeServerPort)
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

func serveEdgeUI(edgeManager *edge.EdgeManager, clusterService agent.ClusterService, serverAddr, serverPort string) {
	edgeServer := http.NewEdgeServer(edgeManager, clusterService)

	go func() {
		log.Printf("[INFO] [main,edge,http] [server_address: %s] [server_port: %s] [message: Starting Edge server]", serverAddr, serverPort)

		err := edgeServer.Start(serverAddr, serverPort)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,http] [message: Unable to start Edge server] [error: %s]", err)
		}

		log.Println("[INFO] [main,edge,http] [message: Edge server shutdown]")
	}()

	go func() {
		timer1 := time.NewTimer(agent.DefaultEdgeSecurityShutdown * time.Minute)
		<-timer1.C

		if !edgeManager.IsKeySet() {
			log.Printf("[INFO] [main,edge,http] [message: Shutting down Edge UI server as no key was specified after %d minutes]", agent.DefaultEdgeSecurityShutdown)
			edgeServer.Shutdown()
		}
	}()
}
