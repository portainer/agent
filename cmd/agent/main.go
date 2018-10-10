package main

import (
	"log"
	"net"
	"os"
	"regexp"
	"runtime"
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
	// TODO: determine a cleaner way to retrieve the container IP that will be used
	// to communicate with other agents.
	// This IP address is also used in the self-signed TLS certificates generation process.
	// Must match the container IP in the overlay network when used inside a Swarm.
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	var advertiseAddr string
	for _, i := range ifaces {
		if matched, _ := regexp.MatchString(`^(eth0)$|^(vEthernet) \(.*\)$`, i.Name); matched {
			addrs, err := i.Addrs()
			if err != nil {
				return "", err
			}

			j := 0
			// On Windows first IP address is link-local IPv6
			if runtime.GOOS == "windows" {
				j = 1
			}

			var ip net.IP
			switch v := addrs[j].(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip.String() != `127.0.0.1` {
				advertiseAddr = ip.String()
				break
			}
		}
	}

	if advertiseAddr == "" {
		return "", agent.ErrRetrievingAdvertiseAddr
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

	signatureService := crypto.NewECDSAService()

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

	systemService := ghw.NewSystemService()

	listenAddr := agent.DefaultListenAddr + ":" + options.Port
	log.Printf("[INFO] - Starting Portainer agent version %s on %s (cluster mode: %t)", agent.AgentVersion, listenAddr, clusterMode)
	server := http.NewServer(systemService, clusterService, signatureService, agentTags)
	server.Start(listenAddr)
}
