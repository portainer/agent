package main

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
	"github.com/portainer/agent/http/client"
	"github.com/portainer/agent/internal/edge"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/logutils"
	"github.com/portainer/agent/net"
	"github.com/portainer/agent/os"
	cluster "github.com/portainer/agent/serf"
)

func main() {
	// Generic

	options, err := parseOptions()
	if err != nil {
		log.Fatalf("[ERROR] [main,configuration] [message: Invalid agent configuration] [error: %s]", err)
	}

	logutils.SetupLogger(options.LogLevel)

	systemService := ghw.NewSystemService(agent.HostRoot)
	containerPlatform := os.DetermineContainerPlatform()
	runtimeConfiguration := &agent.RuntimeConfiguration{
		AgentPort: options.AgentServerPort,
	}

	var clusterService agent.ClusterService
	var dockerInfoService agent.DockerInfoService
	var advertiseAddr string
	var kubeClient *kubernetes.KubeClient

	// !Generic

	// Docker

	if containerPlatform == agent.PlatformDocker {
		log.Println("[INFO] [main] [message: Agent running on Docker platform]")

		dockerInfoService = docker.NewInfoService()

		runtimeConfiguration, err = dockerInfoService.GetRuntimeConfigurationFromDockerEngine()
		if err != nil {
			log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve information from Docker] [error: %s]", err)
		}

		runtimeConfiguration.AgentPort = options.AgentServerPort
		log.Printf("[DEBUG] [main,configuration] [Member tags: %+v]", runtimeConfiguration)

		clusterMode := false
		if runtimeConfiguration.DockerConfiguration.EngineStatus == agent.EngineStatusSwarm {
			clusterMode = true
			log.Println("[INFO] [main] [message: Agent running on a Swarm cluster node. Running in cluster mode]")
		}

		containerName, err := os.GetHostName()
		if err != nil {
			log.Fatalf("[ERROR] [main,os] [message: Unable to retrieve container name] [error: %s]", err)
		}

		advertiseAddr, err = dockerInfoService.GetContainerIpFromDockerEngine(containerName, clusterMode)
		if err != nil {
			log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve local agent IP address] [error: %s]", err)
		}

		if clusterMode {
			clusterService = cluster.NewClusterService(runtimeConfiguration)

			clusterAddr := options.ClusterAddress
			if clusterAddr == "" {
				serviceName, err := dockerInfoService.GetServiceNameFromDockerEngine(containerName)
				if err != nil {
					log.Fatalf("[ERROR] [main,docker] [message: Unable to retrieve agent service name from Docker] [error: %s]", err)
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
	}

	// !Docker

	// Kubernetes
	if containerPlatform == agent.PlatformKubernetes {
		log.Println("[INFO] [main] [message: Agent running on Kubernetes platform]")
		kubeClient, err = kubernetes.NewKubeClient()
		if err != nil {
			log.Fatalf("[ERROR] [main,kubernetes] [message: Unable to create Kubernetes client] [error: %s]", err)
		}

		clusterService = cluster.NewClusterService(runtimeConfiguration)

		advertiseAddr = os.GetKubernetesPodIP()
		if advertiseAddr == "" {
			log.Fatalf("[ERROR] [main,kubernetes,env] [message: KUBERNETES_POD_IP env var must be specified when running on Kubernetes] [error: %s]", err)
		}

		clusterAddr := options.ClusterAddress
		if clusterAddr == "" {
			clusterAddr = "s-portainer-agent-headless"
		}

		// TODO: Workaround. Kubernetes only adds entries in the DNS for running containers. We need to wait a bit
		// for the container to be considered running by Kubernetes and an entry to be added to the DNS.
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
	// !Kubernetes

	// Security

	var signatureService agent.DigitalSignatureService
	if !options.EdgeMode {
		signatureService = crypto.NewECDSAService(options.SharedSecret)
		tlsService := crypto.TLSService{}

		err := tlsService.GenerateCertsForHost(advertiseAddr)
		if err != nil {
			log.Fatalf("[ERROR] [main,tls] [message: Unable to generate self-signed certificates] [error: %s]", err)
		}
	}

	// !Security

	// Edge
	edgeManagerParameters := &edge.ManagerParameters{
		Options:           options,
		AdvertiseAddr:     advertiseAddr,
		ClusterService:    clusterService,
		DockerInfoService: dockerInfoService,
		ContainerPlatform: containerPlatform,
	}
	edgeManager := edge.NewManager(edgeManagerParameters)

	if options.EdgeMode {
		edgeKey, err := retrieveEdgeKey(options.EdgeKey, clusterService)
		if err != nil {
			log.Printf("[ERROR] [main,edge] [message: Unable to retrieve Edge key] [error: %s]", err)
		}

		if edgeKey != "" {
			log.Println("[DEBUG] [main,edge] [message: Edge key found in environment. Associating Edge key]")

			err := edgeManager.SetKey(edgeKey)
			if err != nil {
				log.Fatalf("[ERROR] [main,edge] [message: Unable to associate Edge key] [error: %s]", err)
			}

			err = edgeManager.Start()
			if err != nil {
				log.Fatalf("[ERROR] [main,edge] [message: Unable to start Edge manager] [error: %s]", err)
			}

		} else {
			log.Println("[DEBUG] [main,edge] [message: Edge key not specified. Serving Edge UI]")

			serveEdgeUI(edgeManager, options.EdgeServerAddr, options.EdgeServerPort)
		}
	}

	// !Edge

	// API

	config := &http.APIServerConfig{
		Addr:                 options.AgentServerAddr,
		Port:                 options.AgentServerPort,
		SystemService:        systemService,
		ClusterService:       clusterService,
		EdgeManager:          edgeManager,
		SignatureService:     signatureService,
		RuntimeConfiguration: runtimeConfiguration,
		AgentOptions:         options,
		KubeClient:           kubeClient,
		ContainerPlatform:    containerPlatform,
	}

	if edgeManager.IsEdgeModeEnabled() {
		config.Addr = advertiseAddr
	}

	err = startAPIServer(config)
	if err != nil {
		log.Fatalf("[ERROR] [main,http] [message: Unable to start Agent API server] [error: %s]", err)
	}

	// !API
}

func startAPIServer(config *http.APIServerConfig) error {
	server := http.NewAPIServer(config)

	if config.EdgeManager.IsEdgeModeEnabled() {
		return server.StartUnsecured()
	}

	return server.StartSecured()
}

func parseOptions() (*agent.Options, error) {
	optionParser := os.NewEnvOptionParser()
	return optionParser.Options()
}

func serveEdgeUI(edgeManager *edge.Manager, serverAddr, serverPort string) {
	edgeServer := http.NewEdgeServer(edgeManager)

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

func retrieveEdgeKey(edgeKey string, clusterService agent.ClusterService) (string, error) {

	if edgeKey != "" {
		log.Println("[INFO] [main,edge] [message: Edge key loaded from options]")
		return edgeKey, nil
	}

	var keyRetrievalError error

	edgeKey, keyRetrievalError = retrieveEdgeKeyFromFilesystem()
	if keyRetrievalError != nil {
		return "", keyRetrievalError
	}

	if edgeKey == "" && clusterService != nil {
		edgeKey, keyRetrievalError = retrieveEdgeKeyFromCluster(clusterService)
		if keyRetrievalError != nil {
			return "", keyRetrievalError
		}
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromFilesystem() (string, error) {
	var edgeKey string

	edgeKeyFilePath := fmt.Sprintf("%s/%s", agent.DataDirectory, agent.EdgeKeyFile)

	keyFileExists, err := filesystem.FileExists(edgeKeyFilePath)
	if err != nil {
		return "", err
	}

	if keyFileExists {
		filesystemKey, err := filesystem.ReadFromFile(edgeKeyFilePath)
		if err != nil {
			return "", err
		}

		log.Println("[INFO] [main,edge] [message: Edge key loaded from the filesystem]")
		edgeKey = string(filesystemKey)
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromCluster(clusterService agent.ClusterService) (string, error) {
	var edgeKey string

	member := clusterService.GetMemberWithEdgeKeySet()
	if member != nil {
		httpCli := client.NewAPIClient()

		memberAddr := fmt.Sprintf("%s:%s", member.IPAddress, member.Port)
		memberKey, err := httpCli.GetEdgeKey(memberAddr)
		if err != nil {
			log.Printf("[ERROR] [main,edge,http,cluster] [message: Unable to retrieve Edge key from cluster member] [error: %s]", err)
			return "", err
		}

		log.Println("[INFO] [main,edge] [message: Edge key loaded from cluster]")
		edgeKey = memberKey
	}

	return edgeKey, nil
}
