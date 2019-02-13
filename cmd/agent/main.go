package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/logutils"
	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
	cluster "github.com/portainer/agent/serf"
)

func initOptionsFromEnvironment(clusterMode bool) (*agent.AgentOptions, error) {
	options := &agent.AgentOptions{
		Port: agent.DefaultAgentPort,
		HostManagementEnabled: false,
	}

	clusterAddressEnv := os.Getenv("AGENT_CLUSTER_ADDR")
	if clusterAddressEnv == "" && clusterMode {
		return nil, agent.ErrEnvClusterAddressRequired
	}
	options.ClusterAddress = clusterAddressEnv

	agentPortEnv := os.Getenv("AGENT_PORT")
	if agentPortEnv != "" {
		_, err := strconv.Atoi(agentPortEnv)
		if err != nil {
			return nil, agent.ErrInvalidEnvPortFormat
		}
		options.Port = agentPortEnv
	}

	if os.Getenv("CAP_HOST_MANAGEMENT") == "1" {
		options.HostManagementEnabled = true
	}

	options.SharedSecret = os.Getenv("AGENT_SECRET")

	return options, nil
}

func setupLogging() {

	logLevel := agent.DefaultLogLevel
	logLevelEnv := os.Getenv("LOG_LEVEL")
	if logLevelEnv != "" {
		logLevel = logLevelEnv
	}

	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR"},
		MinLevel: logutils.LogLevel(strings.ToUpper(logLevel)),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)
}

func retrieveInformationFromDockerEnvironment() (map[string]string, error) {
	infoService := docker.InfoService{}
	agentTags, err := infoService.GetInformationFromDockerEngine()
	if err != nil {
		return nil, err
	}

	return agentTags, nil
}

func retrieveAdvertiseAddress() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}

	infoService := docker.InfoService{}
	advertiseAddr, err := infoService.GetContainerIpFromDockerEngine(hostname)
	if err != nil {
		return "", err
	}

	return advertiseAddr, nil
}

func main() {
	setupLogging()

	agentTags, err := retrieveInformationFromDockerEnvironment()
	if err != nil {
		log.Fatalf("[ERROR] - Unable to retrieve information from Docker: %s", err)
	}

	clusterMode := false
	if agentTags[agent.ApplicationTagMode] == "swarm" {
		clusterMode = true
	}

	options, err := initOptionsFromEnvironment(clusterMode)
	if err != nil {
		log.Fatalf("[ERROR] - Error during agent initialization: %s", err)
	}
	agentTags[agent.MemberTagKeyAgentPort] = options.Port

	log.Printf("[DEBUG] - Agent details: %v\n", agentTags)

	advertiseAddr, err := retrieveAdvertiseAddress()
	if err != nil {
		log.Fatalf("[ERROR] - Unable to retrieve advertise address: %s", err)
	}
	log.Printf("[DEBUG] - Using cluster address: %s\n", options.ClusterAddress)
	log.Printf("[DEBUG] - Using advertiseAddr: %s\n", advertiseAddr)

	TLSService := crypto.TLSService{}
	log.Println("[DEBUG] - Generating TLS files...")
	TLSService.GenerateCertsForHost(advertiseAddr)

	signatureService := crypto.NewECDSAService(options.SharedSecret)

	log.Printf("[DEBUG] - Using agent port: %s\n", options.Port)

	var clusterService agent.ClusterService
	if clusterMode {
		clusterService = cluster.NewClusterService()

		// TODO: looks like the Docker DNS cannot find any info on tasks.<service_name>
		// sometimes... Waiting a bit before starting the discovery seems to solve the problem.
		time.Sleep(3 * time.Second)

		err = clusterService.Create(advertiseAddr, options.ClusterAddress, agentTags)
		if err != nil {
			log.Fatalf("[ERROR] - Unable to create cluster: %s", err)
		}
		defer clusterService.Leave()
	}

	systemService := ghw.NewSystemService("/host")

	listenAddr := agent.DefaultListenAddr + ":" + options.Port
	log.Printf("[INFO] - Starting Portainer agent version %s on %s (cluster mode: %t)", agent.AgentVersion, listenAddr, clusterMode)
	server := http.NewServer(systemService, clusterService, signatureService, agentTags, options)
	server.Start(listenAddr)
}
