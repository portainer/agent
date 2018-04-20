package main // import "bitbucket.org/portainer/agent"

import (
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/portainer/agent"
	"bitbucket.org/portainer/agent/crypto"
	"bitbucket.org/portainer/agent/docker"
	"bitbucket.org/portainer/agent/http"
	cluster "bitbucket.org/portainer/agent/serf"
	"github.com/hashicorp/logutils"
)

func initOptionsFromEnvironment() (*agent.AgentOptions, error) {
	options := &agent.AgentOptions{
		Port:     agent.DefaultAgentPort,
		LogLevel: agent.DefaultLogLevel,
	}

	clusterAddressEnv := os.Getenv("AGENT_CLUSTER_ADDR")
	if clusterAddressEnv == "" {
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

	logLevelEnv := os.Getenv("LOG_LEVEL")
	if logLevelEnv != "" {
		options.LogLevel = logLevelEnv
	}

	return options, nil
}

func setupLogging(logLevel string) {
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
	// TODO: determine a cleaner way to retrieve the container IP that will be used
	// to communicate with other agents.
	// Must be container IP in overlay when used inside a Swarm.
	// What about outside of Swarm (e.g. on Standalone engine) ?
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	var advertiseAddr string
	for _, i := range ifaces {
		if i.Name == "eth0" {
			var ip net.IP
			addrs, _ := i.Addrs()
			switch v := addrs[0].(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			advertiseAddr = ip.String()
		}
	}

	if advertiseAddr == "" {
		return "", agent.ErrRetrievingAdvertiseAddr
	}

	return advertiseAddr, nil
}

func main() {
	options, err := initOptionsFromEnvironment()
	if err != nil {
		log.Fatalf("[ERROR] - Error during agent initialization: %s", err)
	}

	setupLogging(options.LogLevel)

	log.Printf("[DEBUG] - Using agent port: %s\n", options.Port)
	log.Printf("[DEBUG] - Using cluster address: %s\n", options.ClusterAddress)

	advertiseAddr, err := retrieveAdvertiseAddress()
	if err != nil {
		log.Fatalf("[ERROR] - Unable to retrieve advertise address: %s", err)
	}
	log.Printf("[DEBUG] - Using advertiseAddr: %s\n", advertiseAddr)

	TLSService := crypto.TLSService{}
	log.Println("[DEBUG] - Generating TLS files...")
	TLSService.GenerateCertsForHost(advertiseAddr)

	agentTags, err := retrieveInformationFromDockerEnvironment()
	if err != nil {
		log.Fatalf("[ERROR] - Unable to retrieve information from Docker: %s", err)
	}
	agentTags[agent.MemberTagKeyAgentPort] = options.Port
	log.Printf("[DEBUG] - Agent details: %v\n", agentTags)

	signatureService := crypto.NewECDSAService()

	// TODO: looks like the Docker DNS cannot find any info on tasks.<service_name>
	// sometimes... Waiting a bit before starting the discovery seems to solve the problem.
	time.Sleep(3 * time.Second)

	clusterService := cluster.NewClusterService()
	err = clusterService.Create(advertiseAddr, options.ClusterAddress, agentTags)
	if err != nil {
		log.Fatalf("[ERROR] - Unable to create cluster: %s", err)
	}
	defer clusterService.Leave()

	server := http.NewServer(clusterService, signatureService, agentTags)
	listenAddr := agent.DefaultListenAddr + ":" + options.Port
	server.Start(listenAddr)
}
