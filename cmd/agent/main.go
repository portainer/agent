package main

import (
	"log"
	"math/rand"
	"time"

	"github.com/portainer/agent/http/client"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
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

	infoService := docker.InfoService{}
	agentTags, err := retrieveInformationFromDockerEnvironment(&infoService)
	if err != nil {
		log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve information from Docker] [error: %s]", err)
	}
	agentTags[agent.MemberTagKeyAgentPort] = options.AgentServerPort
	log.Printf("[DEBUG] [main,configuration] [Member tags: %+v]", agentTags)

	clusterMode := false
	if agentTags[agent.ApplicationTagMode] == "swarm" {
		clusterMode = true
	}

	if options.ClusterAddress == "" && clusterMode {
		log.Fatalf("[ERROR] [main,configuration] [message: AGENT_CLUSTER_ADDR environment variable is required when deploying the agent inside a Swarm cluster]")
	}

	advertiseAddr, err := retrieveAdvertiseAddress(&infoService)
	if err != nil {
		log.Fatalf("[ERROR] [main,docker,os] [message: Unable to retrieve local agent IP address] [error: %s]", err)
	}

	startEdgeProcess := options.EdgeMode
	var clusterService agent.ClusterService
	if clusterMode {

		clusterService = cluster.NewClusterService()
		startEdgeProcess = false

		// TODO: Workaround. looks like the Docker DNS cannot find any info on tasks.<service_name>
		// sometimes... Waiting a bit before starting the discovery seems to solve the problem.
		// This is also randomize to potentially prevent multiple agents started in Edge mode to discover
		// themselves at the same time, preventing one to being elected as the cluster initiator.
		// Should be replaced by a proper way to select a single node inside the cluster.
		sleep(3, 6)

		joinAddr, err := net.LookupIPAddresses(options.ClusterAddress)
		if err != nil {
			log.Fatalf("[ERROR] [main,net] [host: %s] [message: Unable to retrieve a list of IP associated to the host] [error: %s]", options.ClusterAddress, err)
		}

		contactedNodeCount, err := clusterService.Create(advertiseAddr, joinAddr, agentTags)
		if err != nil {
			log.Fatalf("[ERROR] [main,cluster] [message: Unable to create cluster] [error: %s]", err)
		}

		if contactedNodeCount == 1 && options.EdgeMode {
			log.Println("[DEBUG] [main,edge] [message: Cluster initiator. Will manage Edge startup]")
			startEdgeProcess = true
		}

		defer clusterService.Leave()
	}

	log.Printf("[DEBUG] [main,configuration] [agent_port: %s] [cluster_address: %s] [advertise_address: %s]", options.AgentServerPort, options.ClusterAddress, advertiseAddr)

	if startEdgeProcess {
		err := enableEdgeMode(options)
		if err != nil {
			log.Fatalf("[ERROR] [main,edge,rtunnel] [message: Unable to start agent in Edge mode] [error: %s]", err)
		}
	}

	systemService := ghw.NewSystemService("/host")

	var signatureService agent.DigitalSignatureService
	if !options.EdgeMode {
		signatureService = crypto.NewECDSAService(options.SharedSecret)
		tlsService := crypto.TLSService{}

		err := tlsService.GenerateCertsForHost(advertiseAddr)
		if err != nil {
			log.Fatalf("[ERROR] [main,tls] [message: Unable to generate self-signed certificates] [error: %s]", err)
		}
	}

	config := &http.ServerConfig{
		Addr:             options.AgentServerAddr,
		Port:             options.AgentServerPort,
		SystemService:    systemService,
		ClusterService:   clusterService,
		SignatureService: signatureService,
		AgentTags:        agentTags,
		AgentOptions:     options,
		Secured:          !options.EdgeMode,
	}

	log.Printf("[INFO] [http] [server_addr: %s] [server_port: %s] [secured: %t] [cluster_mode: %t] [version: %s] [message: Starting Agent API server]", config.Addr, config.Port, config.Secured, clusterMode, agent.Version)

	err = startAPIServer(config)
	if err != nil {
		log.Fatalf("[ERROR] [main,http] [message: Unable to start Agent API server] [error: %s]", err)
	}
}

func startAPIServer(config *http.ServerConfig) error {
	server := http.NewServer(config)

	if !config.Secured {
		return server.StartUnsecured()
	}

	return server.StartSecured()
}

func enableEdgeMode(options *agent.Options) error {
	tunnelOperator := client.NewTunnelOperator(options.EdgeTunnelServerAddr, options.EdgePollInterval)

	if options.EdgeKey != "" {
		log.Println("[DEBUG] [main,edge] [message: Edge key specified. Starting tunnel operator.]")

		err := tunnelOperator.SetKey(options.EdgeKey)
		if err != nil {
			return err
		}

		return tunnelOperator.Start()
	}

	log.Println("[DEBUG] [main,edge] [message: Edge key not specified. Serving Edge UI]")
	edgeServer := http.NewEdgeServer(tunnelOperator)

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
			log.Printf("[INFO] [main,edge,http] [message: Shutting down file server as no key was specified after %d minutes]", agent.DefaultEdgeSecurityShutdown)
			edgeServer.Shutdown()
		}

		// TODO: if started in Edge mode and no key specified and UI shutdown, should we disable the API server?
	}()

	return nil
}

func sleep(min, max int) {
	sleepDuration := rand.Intn(max-min) + min
	log.Printf("[DEBUG] [main] [sleep: %d]", sleepDuration)
	time.Sleep(time.Duration(sleepDuration) * time.Second)
}

func parseOptions() (*agent.Options, error) {
	optionParser := os.NewEnvOptionParser()
	return optionParser.Options()
}

func retrieveInformationFromDockerEnvironment(infoService agent.InfoService) (map[string]string, error) {
	agentTags, err := infoService.GetInformationFromDockerEngine()
	if err != nil {
		return nil, err
	}

	return agentTags, nil
}

func retrieveAdvertiseAddress(infoService agent.InfoService) (string, error) {
	containerName, err := os.GetHostName()
	if err != nil {
		return "", err
	}

	advertiseAddr, err := infoService.GetContainerIpFromDockerEngine(containerName)
	if err != nil {
		return "", err
	}

	return advertiseAddr, nil
}
