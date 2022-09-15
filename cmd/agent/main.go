package main

import (
	"errors"
	"fmt"
	"log"
	gohttp "net/http"
	goos "os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/edge"
	httpEdge "github.com/portainer/agent/edge/http"
	"github.com/portainer/agent/edge/registry"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/ghw"
	"github.com/portainer/agent/http"
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
		log.Fatalf("[ERROR] [main] [message: Invalid agent configuration] [error: %s]", err)
	}

	if options.EdgeAsyncMode && !options.EdgeMode {
		log.Fatalf("[ERROR] [main] [message: Edge Async mode cannot be enabled, if Edge Mode is disabled]")
	}

	logutils.SetupLogger(options.LogLevel)

	if options.SSLCert != "" && options.SSLKey != "" && options.CertRetryInterval > 0 {
		edge.BlockUntilCertificateIsReady(options.SSLCert, options.SSLKey, options.CertRetryInterval)
	}

	systemService := ghw.NewSystemService(agent.HostRoot)
	containerPlatform := os.DetermineContainerPlatform()
	runtimeConfiguration := &agent.RuntimeConfiguration{
		AgentPort: options.AgentServerPort,
	}

	var clusterService agent.ClusterService
	var dockerInfoService agent.DockerInfoService
	var advertiseAddr string
	var kubeClient *kubernetes.KubeClient
	var nomadConfig agent.NomadConfig

	// !Generic

	// Docker & Podman

	if containerPlatform == agent.PlatformDocker || containerPlatform == agent.PlatformPodman {
		log.Println("[INFO] [main] [message: Agent running on Docker platform]")

		dockerInfoService = docker.NewInfoService()

		runtimeConfiguration, err = dockerInfoService.GetRuntimeConfigurationFromDockerEngine()
		if err != nil {
			log.Fatalf("[ERROR] [main] [message: Unable to retrieve information from Docker] [error: %s]", err)
		}

		runtimeConfiguration.AgentPort = options.AgentServerPort
		log.Printf("[DEBUG] [main] [Member tags: %+v]", runtimeConfiguration)

		clusterMode := false
		if runtimeConfiguration.DockerConfiguration.EngineStatus == agent.EngineStatusSwarm {
			if options.ClusterModeEnabled {
				clusterMode = true
				log.Println("[INFO] [main] [message: Agent running on a Swarm cluster node. Running in cluster mode]")
			} else {
				log.Println("[INFO] [main] [message: Detected a Swarm cluster node. Although, Cluster mode is disabled.]")
			}
		}

		containerName, err := os.GetHostName()
		if err != nil {
			log.Fatalf("[ERROR] [main] [message: Unable to retrieve container name] [error: %s]", err)
		}

		advertiseAddr, err = dockerInfoService.GetContainerIpFromDockerEngine(containerName, clusterMode)
		if err != nil {
			log.Printf("[WARN] [main] [message: Unable to retrieve agent container IP address, using '%s' instead] [error: %s]", options.AgentServerAddr, err)
			advertiseAddr = options.AgentServerAddr
		}

		if containerPlatform == agent.PlatformDocker && clusterMode {
			clusterService = cluster.NewClusterService(runtimeConfiguration)

			clusterAddr := options.ClusterAddress
			if clusterAddr == "" {
				serviceName, err := dockerInfoService.GetServiceNameFromDockerEngine(containerName)
				if err != nil {
					log.Fatalf("[ERROR] [main] [message: Unable to retrieve agent service name from Docker] [error: %s]", err)
				}

				clusterAddr = fmt.Sprintf("tasks.%s", serviceName)
			}

			// TODO: Workaround. looks like the Docker DNS cannot find any info on tasks.<service_name>
			// sometimes... Waiting a bit before starting the discovery (at least 3 seconds) seems to solve the problem.
			time.Sleep(3 * time.Second)

			joinAddr, err := net.LookupIPAddresses(clusterAddr)
			if err != nil {
				log.Fatalf("[ERROR] [main] [host: %s] [message: Unable to retrieve a list of IP associated to the host] [error: %s]", clusterAddr, err)
			}

			err = clusterService.Create(advertiseAddr, joinAddr, options.ClusterProbeTimeout, options.ClusterProbeInterval)
			if err != nil {
				log.Fatalf("[ERROR] [main] [message: Unable to create cluster] [error: %s]", err)
			}

			log.Printf("[DEBUG] [main] [agent_port: %s] [cluster_address: %s] [advertise_address: %s] [probe_timeout: %s] [probe_interval: %s]", options.AgentServerPort, clusterAddr, advertiseAddr, options.ClusterProbeTimeout, options.ClusterProbeInterval)

			defer clusterService.Leave()
		}
	}

	// !Docker

	// Kubernetes
	var kubernetesDeployer *exec.KubernetesDeployer
	if containerPlatform == agent.PlatformKubernetes {
		log.Println("[INFO] [main] [message: Agent running on Kubernetes platform]")
		kubeClient, err = kubernetes.NewKubeClient()
		if err != nil {
			log.Fatalf("[ERROR] [main] [message: Unable to create Kubernetes client] [error: %s]", err)
		}

		kubernetesDeployer = exec.NewKubernetesDeployer(options.AssetsPath)

		clusterService = cluster.NewClusterService(runtimeConfiguration)

		advertiseAddr = os.GetKubernetesPodIP()
		if advertiseAddr == "" {
			log.Fatalf("[ERROR] [main] [message: KUBERNETES_POD_IP env var must be specified when running on Kubernetes] [error: %s]", err)
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
			log.Fatalf("[ERROR] [main] [host: %s] [message: Unable to retrieve a list of IP associated to the host] [error: %s]", clusterAddr, err)
		}

		err = clusterService.Create(advertiseAddr, joinAddr, options.ClusterProbeTimeout, options.ClusterProbeInterval)
		if err != nil {
			log.Fatalf("[ERROR] [main] [message: Unable to create cluster] [error: %s]", err)
		}

		log.Printf("[DEBUG] [main] [agent_port: %s] [cluster_address: %s] [advertise_address: %s] [probe_timeout: %s] [probe_interval: %s]", options.AgentServerPort, clusterAddr, advertiseAddr, options.ClusterProbeTimeout, options.ClusterProbeInterval)

		defer clusterService.Leave()
	}
	// !Kubernetes

	// Nomad
	if containerPlatform == agent.PlatformNomad {
		advertiseAddr, err = net.GetLocalIP()
		if err != nil {
			log.Fatalf("[ERROR] [main,nomad] [message: Unable to retrieve local IP associated to the agent] [error: %s]", err)
		}

		nomadConfig.NomadAddr = goos.Getenv(agent.NomadAddrEnvVarName)
		if nomadConfig.NomadAddr == "" {
			log.Fatalf("[ERROR] [main,nomad] [message: Unable to retrieve environment variable NOMAD_ADDR]")
		}

		if strings.HasPrefix(nomadConfig.NomadAddr, "https") {
			nomadConfig.NomadTLSEnabled = true

			// Write the TLS certificate into files and update the paths to nomadConfig for Reversy Tunnel API use
			nomadCACertContent := goos.Getenv(agent.NomadCACertContentEnvVarName)
			if len(nomadCACertContent) == 0 {
				log.Fatalf("[ERROR] [main] [message: Nomad CA Certificate is not exported] [error: %s]", err)
			} else {
				err = filesystem.WriteFile(options.DataPath, agent.NomadTLSCACertPath, []byte(nomadCACertContent), 0600)
				if err != nil {
					log.Fatalf("[ERROR] [main] [message: Fail to write the Nomad CA Certificate] [error: %s]", err)
				}
				nomadConfig.NomadCACert = path.Join(options.DataPath, agent.NomadTLSCACertPath)
			}

			nomadClientCertContent := goos.Getenv(agent.NomadClientCertContentEnvVarName)
			if len(nomadClientCertContent) == 0 {
				log.Fatalf("[ERROR] [main] [message: Nomad Client Certificate is not exported] [error: %s]", err)
			} else {
				err = filesystem.WriteFile(options.DataPath, agent.NomadTLSCertPath, []byte(nomadClientCertContent), 0600)
				if err != nil {
					log.Fatalf("[ERROR] [main] [message: Fail to write the Nomad Client Certificate] [error: %s]", err)
				}
				nomadConfig.NomadClientCert = path.Join(options.DataPath, agent.NomadTLSCertPath)
			}

			nomadClientKeyContent := goos.Getenv(agent.NomadClientKeyContentEnvVarName)
			if len(nomadClientKeyContent) == 0 {
				log.Fatalf("[ERROR] [main] [message: Nomad Client Key is not exported] [error: %s]", err)
			} else {
				err = filesystem.WriteFile(options.DataPath, agent.NomadTLSKeyPath, []byte(nomadClientKeyContent), 0600)
				if err != nil {
					log.Fatalf("[ERROR] [main] [message: Fail to write the Nomad Client Key] [error: %s]", err)
				}
				nomadConfig.NomadClientKey = path.Join(options.DataPath, agent.NomadTLSKeyPath)
			}

			if _, err := goos.Stat(nomadConfig.NomadCACert); errors.Is(err, goos.ErrNotExist) {
				log.Fatalf("[ERROR] [main] [message: Unable to locate the Nomad CA Certificate] [error: %s]", err)
			}
			if _, err := goos.Stat(nomadConfig.NomadClientCert); errors.Is(err, goos.ErrNotExist) {
				log.Fatalf("[ERROR] [main] [message: Unable to locate the Nomad Client Certificate] [error: %s]", err)
			}
			if _, err := goos.Stat(nomadConfig.NomadClientKey); errors.Is(err, goos.ErrNotExist) {
				log.Fatalf("[ERROR] [main] [message: Unable to locate the Nomad Client Key] [error: %s]", err)
			}

			// Export the TLS certificates path for Nomad Edge Deployer
			goos.Setenv(agent.NomadCACertEnvVarName, nomadConfig.NomadCACert)
			goos.Setenv(agent.NomadClientCertEnvVarName, nomadConfig.NomadClientCert)
			goos.Setenv(agent.NomadClientKeyEnvVarName, nomadConfig.NomadClientKey)
		}

		nomadConfig.NomadToken = goos.Getenv(agent.NomadTokenEnvVarName)

		log.Printf("[DEBUG] [main,configuration] [agent_port: %s] [advertise_address: %s] [NomadAddr: %s]", options.AgentServerPort, advertiseAddr, nomadConfig.NomadAddr)
	}
	// !Nomad

	// Security
	signatureService := crypto.NewECDSAService(options.SharedSecret)

	if !options.EdgeMode {
		tlsService := crypto.TLSService{}

		err := tlsService.GenerateCertsForHost(advertiseAddr)
		if err != nil {
			log.Fatalf("[ERROR] [main] [message: Unable to generate self-signed certificates] [error: %s]", err)
		}
	}

	// !Security

	// Edge
	var edgeManager *edge.Manager
	if options.EdgeMode {
		edgeManagerParameters := &edge.ManagerParameters{
			Options:           options,
			AdvertiseAddr:     advertiseAddr,
			ClusterService:    clusterService,
			DockerInfoService: dockerInfoService,
			ContainerPlatform: containerPlatform,
		}
		edgeManager = edge.NewManager(edgeManagerParameters)

		edgeKey, err := edgeManager.RetrieveEdgeKey(options.EdgeKey, clusterService)
		if err != nil {
			log.Printf("[ERROR] [main] [message: Unable to retrieve Edge key] [error: %s]", err)
		}

		if edgeKey != "" {
			log.Println("[DEBUG] [main] [message: Edge key found in environment. Associating Edge key]")

			err := edgeManager.SetKey(edgeKey)
			if err != nil {
				log.Fatalf("[ERROR] [main] [message: Unable to associate Edge key] [error: %s]", err)
			}

			err = edgeManager.Start()
			if err != nil {
				log.Fatalf("[ERROR] [main] [message: Unable to start Edge manager] [error: %s]", err)
			}

		} else {
			log.Println("[DEBUG] [main] [message: Edge key not specified. Serving Edge UI]")

			serveEdgeUI(edgeManager, options.EdgeUIServerAddr, options.EdgeUIServerPort)
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
		KubernetesDeployer:   kubernetesDeployer,
		ContainerPlatform:    containerPlatform,
		NomadConfig:          nomadConfig,
	}

	if options.EdgeMode {
		config.Addr = advertiseAddr
	}
	err = registry.StartRegistryServer(edgeManager)
	if err != nil {
		log.Fatalf("[ERROR] [main] [message: Unable to start registry server] [error: %s]", err)
	}

	err = startAPIServer(config, options.EdgeMode)
	if err != nil && !errors.Is(err, gohttp.ErrServerClosed) {
		log.Fatalf("[ERROR] [main] [message: Unable to start Agent API server] [error: %s]", err)
	}

	// !API

	sigs := make(chan goos.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	s := <-sigs

	fmt.Printf("[DEBUG] [main] [message: shutting down] [signal: %s]", s)
}

func startAPIServer(config *http.APIServerConfig, edgeMode bool) error {
	server := http.NewAPIServer(config)

	return server.Start(edgeMode)
}

func parseOptions() (*agent.Options, error) {
	optionParser := os.NewEnvOptionParser()
	return optionParser.Options()
}

func serveEdgeUI(edgeManager *edge.Manager, serverAddr, serverPort string) {
	edgeServer := httpEdge.NewEdgeServer(edgeManager)

	go func() {
		log.Printf("[INFO] [main] [server_address: %s] [server_port: %s] [message: Starting Edge UI server]", serverAddr, serverPort)

		err := edgeServer.Start(serverAddr, serverPort)
		if err != nil {
			log.Fatalf("[ERROR] [main] [message: Unable to start Edge server] [error: %s]", err)
		}

		log.Println("[INFO] [main] [message: Edge server shutdown]")
	}()

	go func() {
		time.Sleep(agent.DefaultEdgeSecurityShutdown * time.Minute)

		if !edgeManager.IsKeySet() {
			log.Printf("[INFO] [main] [message: Shutting down Edge UI server as no key was specified after %d minutes]", agent.DefaultEdgeSecurityShutdown)
			edgeServer.Shutdown()
		}
	}()
}
