package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/portainer/agent/chisel"

	"github.com/hashicorp/logutils"
	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
	"github.com/portainer/agent/net"
	cluster "github.com/portainer/agent/serf"
)

// TODO: should be externalised as OptionParser
func initOptionsFromEnvironment(clusterMode bool) (*agent.Options, error) {
	options := &agent.Options{
		AgentServerAddr:       agent.DefaultAgentAddr,
		AgentServerPort:       agent.DefaultAgentPort,
		HostManagementEnabled: false,
		SharedSecret:          os.Getenv("AGENT_SECRET"),
		EdgeServerAddr:        agent.DefaultEdgeServerAddr,
		EdgeServerPort:        agent.DefaultEdgeServerPort,
		EdgeTunnelServerAddr:  os.Getenv("EDGE_TUNNEL_SERVER"),
	}

	if os.Getenv("CAP_HOST_MANAGEMENT") == "1" {
		options.HostManagementEnabled = true
	}

	if os.Getenv("EDGE") == "1" {
		options.EdgeMode = true
	}

	clusterAddressEnv := os.Getenv("AGENT_CLUSTER_ADDR")
	if clusterAddressEnv == "" && clusterMode {
		return nil, agent.ErrEnvClusterAddressRequired
	}
	options.ClusterAddress = clusterAddressEnv

	agentAddrEnv := os.Getenv("AGENT_ADDR")
	if agentAddrEnv != "" {
		options.AgentServerAddr = agentAddrEnv
	}

	agentPortEnv := os.Getenv("AGENT_PORT")
	if agentPortEnv != "" {
		_, err := strconv.Atoi(agentPortEnv)
		if err != nil {
			return nil, agent.ErrInvalidEnvPortFormat
		}
		options.AgentServerPort = agentPortEnv
	}

	edgeAddrEnv := os.Getenv("EDGE_SERVER_ADDR")
	if edgeAddrEnv != "" {
		options.EdgeServerAddr = edgeAddrEnv
	}

	edgePortEnv := os.Getenv("EDGE_SERVER_PORT")
	if edgePortEnv != "" {
		_, err := strconv.Atoi(edgePortEnv)
		if err != nil {
			return nil, agent.ErrInvalidEnvPortFormat
		}
		options.EdgeServerPort = edgePortEnv
	}

	edgeKeyEnv := os.Getenv("EDGE_KEY")
	if edgeKeyEnv != "" {
		options.EdgeKey = edgeKeyEnv
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

func retrieveInformationFromDockerEnvironment(infoService agent.InfoService) (map[string]string, error) {
	agentTags, err := infoService.GetInformationFromDockerEngine()
	if err != nil {
		return nil, err
	}

	return agentTags, nil
}

func retrieveAdvertiseAddress(infoService agent.InfoService) (string, error) {
	containerName, err := os.Hostname()
	if err != nil {
		return "", err
	}

	advertiseAddr, err := infoService.GetContainerIpFromDockerEngine(containerName)
	if err != nil {
		return "", err
	}

	return advertiseAddr, nil
}

func main() {
	setupLogging()

	infoService := docker.InfoService{}
	agentTags, err := retrieveInformationFromDockerEnvironment(&infoService)
	if err != nil {
		log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve information from Docker] [error: %s]", err)
	}

	clusterMode := false
	if agentTags[agent.ApplicationTagMode] == "swarm" {
		clusterMode = true
	}

	options, err := initOptionsFromEnvironment(clusterMode)
	if err != nil {
		log.Fatalf("[ERROR] [main,configuration] [message: Invalid agent configuration] [error: %s]", err)
	}
	agentTags[agent.MemberTagKeyAgentPort] = options.AgentServerPort

	log.Printf("[DEBUG] [main,configuration] [Member tags: %+v]", agentTags)

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
			//edgeServer := http.NewFileServer(options.EdgeServerAddr, options.EdgeServerPort, startChiselClient, options.EdgeTunnelServerAddr)

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

				// TODO: use getter?
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

// TODO: error management
func startChiselClient(key, server string) error {
	//// TODO: should use another options EDGE_KEY and be validated in options parsing
	//decodedKey, err := base64.RawStdEncoding.DecodeString(key)
	//if err != nil {
	//	log.Fatalf("[ERROR] - Invalid AGENT_SECRET: %s", err)
	//}
	//
	//keyInfo := strings.Split(string(decodedKey), ":")
	//tunnelServerAddr := keyInfo[0]
	//tunnelServerPort := keyInfo[1]
	//remotePort := keyInfo[2]
	//fingerprint := keyInfo[3]
	//credentials := strings.Replace(keyInfo[4], "@", ":", -1)
	//
	//log.Printf("[DEBUG] [edge] [tunnel_server_addr: %s] [tunnel_server_port: %d] [remote_port: %s] [server_fingerprint: %s]", tunnelServerAddr, tunnelServerPort, remotePort, fingerprint)
	//
	//// TODO: validation must be done somewhere
	////or options must be injected
	//if tunnelServerAddr == "localhost" {
	//	if server == "" {
	//		log.Fatal("[ERROR] - Tunnel server env var required")
	//	}
	//	tunnelServerAddr = server
	//}
	//
	//// TODO: manage timeout
	//chiselClient, err := chclient.NewClient(&chclient.Config{
	//	Server:      tunnelServerAddr + ":" + tunnelServerPort,
	//	Remotes:     []string{"R:" + remotePort + ":" + "localhost:9001"},
	//	Fingerprint: fingerprint,
	//	Auth:        credentials,
	//})
	//if err != nil {
	//	log.Fatalf("[ERROR] [edge] [message: Unable to create tunnel client] [error: %s]", err)
	//}
	//
	//err = chiselClient.Start(context.Background())
	//if err != nil {
	//	log.Fatalf("[ERROR] [edge] [message: Unable to start tunnel client] [error: %s]", err)
	//}

	return nil
}
