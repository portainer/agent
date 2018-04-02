package main // import "bitbucket.org/portainer/agent"

import (
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"bitbucket.org/portainer/agent"
	"bitbucket.org/portainer/agent/docker"
	"bitbucket.org/portainer/agent/http"
	cluster "bitbucket.org/portainer/agent/serf"
	"github.com/hashicorp/logutils"
)

func main() {

	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR"},
		MinLevel: logutils.LogLevel(agent.DefaultLogLevel),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)

	agentPort := agent.DefaultAgentPort
	agentPortEnv := os.Getenv("AGENT_PORT")
	if agentPortEnv != "" {
		_, err := strconv.Atoi(agentPortEnv)
		if err != nil {
			log.Printf("[ERROR] - Err: %v\n", err)
			log.Fatal("[ERROR] - Invalid port format in AGENT_PORT environment variable.")
		}
		agentPort = agentPortEnv
	}
	log.Printf("[DEBUG] - Using agent port: %s\n", agentPort)

	// Service name should be specified here to use DNS-SRV records.
	// We automatically append "tasks." to discover the other agents.
	clusterJoinAddr := os.Getenv("AGENT_CLUSTER_ADDR")
	if clusterJoinAddr == "" {
		log.Fatal("[ERROR] - AGENT_CLUSTER_ADDR environment variable is required.")
	}
	joinAddr := "tasks." + clusterJoinAddr

	infoService := docker.InfoService{}
	agentTags, err := infoService.GetInformationFromDockerEngine()
	if err != nil {
		log.Printf("[ERROR] - Err: %v\n", err)
		log.Fatal("[ERROR] - Unable to retrieve information from Docker engine")
	}
	agentTags[agent.MemberTagKeyAgentPort] = agentPort
	log.Printf("[DEBUG] - Agent details: %v\n", agentTags)

	// TODO: determine a cleaner way to retrieve the container IP that will be used
	// to communicate with other agents.
	// Must be container IP in overlay when used inside a Swarm.
	// What about outside of Swarm (e.g. on Standalone engine) ?
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("[ERROR] - Err: %v\n", err)
		log.Fatal("[ERROR] - Unable to retrieve network interfaces details")
	}

	advertiseAddr := "0.0.0.0"
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
	log.Printf("[DEBUG] - Using advertiseAddr: %s\n", advertiseAddr)

	// TODO: looks like the Docker DNS cannot find any info on tasks.<service_name>
	// sometimes... Waiting a bit before starting the discovery seems to solve the problem.
	time.Sleep(3 * time.Second)

	clusterService := cluster.NewClusterService()
	err = clusterService.Create(advertiseAddr, joinAddr, agentTags)
	if err != nil {
		log.Printf("[ERROR] - Err: %v\n", err)
		log.Fatal("[ERROR] - Unable to create cluster")
	}
	defer clusterService.Leave()

	server := http.NewServer(clusterService, agentTags)
	listenAddr := agent.DefaultListenAddr + ":" + agentPort
	server.Start(listenAddr)
}
