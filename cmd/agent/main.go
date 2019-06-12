package main

import (
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
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
		log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve IP address used to form the agent cluster] [error: %s]", err)
	}

	// TODO: not necessary in Edge mode
	TLSService := crypto.TLSService{}
	err = TLSService.GenerateCertsForHost(advertiseAddr)
	if err != nil {
		log.Fatalf("[ERROR] [main,tls] [message: Unable to generate self-signed certificates] [error: %s]", err)
	}

	// TODO: not necessary in Edge mode
	signatureService := crypto.NewECDSAService(options.SharedSecret)

	log.Printf("[DEBUG] [main,configuration] [agent_port: %s] [cluster_address: %s] [advertise_address: %s]", options.AgentServerPort, options.ClusterAddress, advertiseAddr)

	triggerEdgeStartup := options.EdgeMode

	var clusterService agent.ClusterService
	if clusterMode {
		triggerEdgeStartup = false

		clusterService = cluster.NewClusterService()

		// TODO: Workaround. looks like the Docker DNS cannot find any info on tasks.<service_name>
		// sometimes... Waiting a bit before starting the discovery seems to solve the problem.
		time.Sleep(3 * time.Second)

		joinAddr, err := net.LookupIPAddresses(options.ClusterAddress)
		if err != nil {
			log.Fatalf("[ERROR] [main,net] [host: %s] [message: Unable to retrieve a list of IP associated to the host] [error: %s]", options.ClusterAddress, err)
		}

		contactedNodeCount, err := clusterService.Create(advertiseAddr, joinAddr, agentTags)
		if err != nil {
			log.Fatalf("[ERROR] [main,cluster] [message: Unable to create cluster] [error: %s]", err)
		}

		if contactedNodeCount == 1 && options.EdgeMode {
			log.Println("[DEBUG] - [main,cluster,edge] - Cluster initiator. Will manage Edge startup.")
			triggerEdgeStartup = true
		}

		defer clusterService.Leave()
	}

	systemService := ghw.NewSystemService("/host")

	// TODO: review this multiple conditions, make it clearer
	if triggerEdgeStartup {
		reverseTunnelClient := chisel.NewClient(options.EdgeTunnelServerAddr)

		if options.EdgeKey == "" {
			edgeServer := http.NewEdgeServer(reverseTunnelClient)

			go func() {
				log.Printf("[INFO] [main,edge,http] [server_address: %s] [server_port: %s] [message: Starting Edge server]", options.EdgeServerAddr, options.EdgeServerPort)

				err := edgeServer.Start(options.EdgeServerAddr, options.EdgeServerPort)
				if err != nil {
					log.Fatalf("[ERROR] [main,edge,http] [message: Unable to start Edge server] [error: %s]", err)
				}

				log.Println("[INFO] [main,edge,http] - [message: Edge server shutdown]")
			}()

			go func() {
				timer1 := time.NewTimer(agent.DefaultEdgeSecurityShutdown * time.Minute)
				<-timer1.C

				if !reverseTunnelClient.IsKeySet() {
					log.Printf("[INFO] [main,edge,http] - [message: Shutting down file server as no key was specified after %d minutes]", agent.DefaultEdgeSecurityShutdown)
					// TODO: error handling?
					edgeServer.Shutdown()
				}

			}()
		} else {
			err = reverseTunnelClient.CreateTunnel(options.EdgeKey)
			if err != nil {
				log.Fatalf("[ERROR] [main,edge,rtunnel] [message: Unable to create reverse tunnel] [error: %s]", err)
			}
		}

	}

	// TODO: if started in Edge mode and no key specified and UI shutdown, should be disabled?
	log.Printf("[INFO] [main,http] [server_addr: %s] [server_port: %s] [cluster_mode: %t] [version: %s]  [message: Starting Agent API server]", options.AgentServerAddr, options.AgentServerPort, clusterMode, agent.Version)

	server := http.NewServer(systemService, clusterService, signatureService, agentTags, options)
	err = server.Start(options.AgentServerAddr, options.AgentServerPort)
	if err != nil {
		log.Fatalf("[ERROR] [main,http] [message: Unable to start Agent API server] [error: %s]", err)
	}
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
