package main // import "bitbucket.org/portainer/agent"

import (
	"context"
	"log"
	"net"
	"os"
	"time"

	"bitbucket.org/portainer/agent"
	"bitbucket.org/portainer/agent/http"
	cluster "bitbucket.org/portainer/agent/serf"
	"github.com/docker/docker/client"
	"github.com/hashicorp/logutils"
)

func main() {

	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR"},
		MinLevel: logutils.LogLevel("DEBUG"),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)

	listenAddr := ":9001"
	listenAddrEnv := os.Getenv("AGENT_ADDR")
	if listenAddrEnv != "" {
		listenAddr = listenAddrEnv
	}
	log.Printf("[DEBUG] - Using listenAddr: %s\n", listenAddr)

	advertiseAddr := "0.0.0.0"
	advertiseAddrEnv := os.Getenv("AGENT_ADV_ADDR")
	if advertiseAddrEnv != "" {
		advertiseAddr = advertiseAddrEnv
	}
	log.Printf("[DEBUG] - Using advertiseAddr: %s\n", advertiseAddr)

	// TODO: determine a cleaner way to retrieve the container IP that will be used
	// to communicate with other agents.
	// Must be container IP in overlay when used inside a Swarm.
	// What about outside of Swarm (e.g. on Standalone engine) ?
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("[ERROR] - Err: %v\n", err)
		log.Fatal("[ERROR] - Unable to retrieve network interfaces details")
	}

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

	cli, err := client.NewClient("unix:///var/run/docker.sock", "1.30", nil, nil)
	if err != nil {
		log.Printf("[ERROR] - Err: %v\n", err)
		log.Fatal("[ERROR] - Unable to start Docker client")
	}

	info, err := cli.Info(context.Background())
	if err != nil {
		log.Printf("[ERROR] - Err: %v\n", err)
		log.Fatal("[ERROR] - Unable to retrieve Docker info from server")
	}

	agentTags := make(map[string]string)
	agentTags[agent.MemberTagKeyNodeAddress] = info.Swarm.NodeAddr
	agentTags[agent.MemberTagKeyNodeRole] = "worker"
	if info.Swarm.ControlAvailable {
		agentTags[agent.MemberTagKeyNodeRole] = "manager"
	}

	// TODO: update the value of MemberTagKeyAgentPort, the listenAddr is injected here
	// and is not the format of a port (e.g. :9001).
	agentTags[agent.MemberTagKeyAgentPort] = listenAddr

	// Service name should be specified here to use DNS-SRV records (automatically append tasks.).
	joinAddr := "tasks." + os.Getenv("AGENT_CLUSTER_ADDR")

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

	server := http.Server{}
	server.ClusterService = clusterService
	server.Start(listenAddr)
}
