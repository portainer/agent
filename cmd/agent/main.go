package main

import (
	"errors"
	"fmt"
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
	"github.com/portainer/agent/healthcheck"
	"github.com/portainer/agent/http"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/net"
	"github.com/portainer/agent/os"
	cluster "github.com/portainer/agent/serf"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

func init() {
	zerolog.ErrorStackFieldName = "stack_trace"
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	log.Logger = log.Logger.With().Caller().Stack().Logger()
}

func main() {
	// Generic

	options, err := parseOptions()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid agent configuration")
	}

	setLoggingLevel(options.LogLevel)

	if options.EdgeAsyncMode && !options.EdgeMode {
		log.Fatal().Msg("edge Async mode cannot be enabled if Edge Mode is disabled")
	}

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
		log.Info().Msg("agent running on Docker platform")

		dockerInfoService = docker.NewInfoService()

		runtimeConfiguration, err = dockerInfoService.GetRuntimeConfigurationFromDockerEngine()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to retrieve information from Docker")
		}

		runtimeConfiguration.AgentPort = options.AgentServerPort
		log.Debug().Str("member_tags", fmt.Sprintf("%+v", runtimeConfiguration)).Msg("")

		clusterMode := false
		if runtimeConfiguration.DockerConfiguration.EngineStatus == agent.EngineStatusSwarm {
			clusterMode = true
			log.Info().Msg("agent running on a Swarm cluster node. Running in cluster mode")
		}

		containerName, err := os.GetHostName()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to retrieve container name")
		}

		advertiseAddr, err = dockerInfoService.GetContainerIpFromDockerEngine(containerName, clusterMode)
		if err != nil {
			log.Warn().Str("host_flag", options.AgentServerAddr).Err(err).
				Msg("unable to retrieve agent container IP address, using host flag instead")

			advertiseAddr = options.AgentServerAddr
		}

		if containerPlatform == agent.PlatformDocker && clusterMode {
			clusterService = cluster.NewClusterService(runtimeConfiguration)

			clusterAddr := options.ClusterAddress
			if clusterAddr == "" {
				serviceName, err := dockerInfoService.GetServiceNameFromDockerEngine(containerName)
				if err != nil {
					log.Fatal().Err(err).Msg("unable to retrieve agent service name from Docker")
				}

				clusterAddr = fmt.Sprintf("tasks.%s", serviceName)
			}

			// TODO: Workaround. looks like the Docker DNS cannot find any info on tasks.<service_name>
			// sometimes... Waiting a bit before starting the discovery (at least 3 seconds) seems to solve the problem.
			time.Sleep(3 * time.Second)

			joinAddr, err := net.LookupIPAddresses(clusterAddr)
			if err != nil {
				log.Fatal().Str("host", clusterAddr).Err(err).
					Msg("unable to retrieve a list of IP associated to the host")
			}

			err = clusterService.Create(advertiseAddr, joinAddr, options.ClusterProbeTimeout, options.ClusterProbeInterval)
			if err != nil {
				log.Fatal().Err(err).Msg("unable to create cluster")
			}

			log.Debug().
				Str("agent_port", options.AgentServerPort).
				Str("cluster_address", clusterAddr).
				Str("advertise_address", advertiseAddr).
				Str("probe_timeout", options.ClusterProbeTimeout.String()).
				Str("probe_interval", options.ClusterProbeInterval.String()).
				Msg("")

			defer clusterService.Leave()
		}
	}

	// !Docker

	// Kubernetes
	var kubernetesDeployer *exec.KubernetesDeployer
	if containerPlatform == agent.PlatformKubernetes {
		log.Info().Msg("agent running on Kubernetes platform")

		kubeClient, err = kubernetes.NewKubeClient()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to create Kubernetes client")
		}

		kubernetesDeployer = exec.NewKubernetesDeployer(options.AssetsPath)

		clusterService = cluster.NewClusterService(runtimeConfiguration)

		advertiseAddr = os.GetKubernetesPodIP()
		if advertiseAddr == "" {
			log.Fatal().Err(err).Msg("KUBERNETES_POD_IP env var must be specified when running on Kubernetes")
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
			log.Fatal().Str("host", clusterAddr).Err(err).
				Msg("unable to retrieve a list of IP associated to the host")
		}

		err = clusterService.Create(advertiseAddr, joinAddr, options.ClusterProbeTimeout, options.ClusterProbeInterval)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to create cluster")
		}

		log.Debug().
			Str("agent_port", options.AgentServerPort).
			Str("cluster_address", clusterAddr).
			Str("advertise_address", advertiseAddr).
			Str("probe_timeout", options.ClusterProbeTimeout.String()).
			Str("probe_interval", options.ClusterProbeInterval.String()).
			Msg("")

		defer clusterService.Leave()
	}
	// !Kubernetes

	// Nomad
	if containerPlatform == agent.PlatformNomad {
		advertiseAddr, err = net.GetLocalIP()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to retrieve local IP associated to the agent")
		}

		nomadConfig.NomadAddr = goos.Getenv(agent.NomadAddrEnvVarName)
		if nomadConfig.NomadAddr == "" {
			log.Fatal().Msg("unable to retrieve environment variable NOMAD_ADDR")
		}

		if strings.HasPrefix(nomadConfig.NomadAddr, "https") {
			nomadConfig.NomadTLSEnabled = true

			// Write the TLS certificate into files and update the paths to nomadConfig for Reversy Tunnel API use
			nomadCACertContent := goos.Getenv(agent.NomadCACertContentEnvVarName)
			if len(nomadCACertContent) == 0 {
				log.Fatal().Err(err).Msg("nomad CA Certificate is not exported")
			}

			err = filesystem.WriteFile(options.DataPath, agent.NomadTLSCACertPath, []byte(nomadCACertContent), 0600)
			if err != nil {
				log.Fatal().Err(err).Msg("fail to write the Nomad CA Certificate")
			}

			nomadConfig.NomadCACert = path.Join(options.DataPath, agent.NomadTLSCACertPath)

			nomadClientCertContent := goos.Getenv(agent.NomadClientCertContentEnvVarName)
			if len(nomadClientCertContent) == 0 {
				log.Fatal().Err(err).Msg("Nomad Client Certificate is not exported")
			}

			err = filesystem.WriteFile(options.DataPath, agent.NomadTLSCertPath, []byte(nomadClientCertContent), 0600)
			if err != nil {
				log.Fatal().Err(err).Msg("fail to write the Nomad Client Certificate")
			}

			nomadConfig.NomadClientCert = path.Join(options.DataPath, agent.NomadTLSCertPath)

			nomadClientKeyContent := goos.Getenv(agent.NomadClientKeyContentEnvVarName)
			if len(nomadClientKeyContent) == 0 {
				log.Fatal().Err(err).Msg("Nomad Client Key is not exported")
			}

			err = filesystem.WriteFile(options.DataPath, agent.NomadTLSKeyPath, []byte(nomadClientKeyContent), 0600)
			if err != nil {
				log.Fatal().Err(err).Msg("fail to write the Nomad Client Key")
			}

			nomadConfig.NomadClientKey = path.Join(options.DataPath, agent.NomadTLSKeyPath)

			if _, err := goos.Stat(nomadConfig.NomadCACert); errors.Is(err, goos.ErrNotExist) {
				log.Fatal().Err(err).Msg("unable to locate the Nomad CA Certificate")
			}

			if _, err := goos.Stat(nomadConfig.NomadClientCert); errors.Is(err, goos.ErrNotExist) {
				log.Fatal().Err(err).Msg("unable to locate the Nomad Client Certificate]")
			}

			if _, err := goos.Stat(nomadConfig.NomadClientKey); errors.Is(err, goos.ErrNotExist) {
				log.Fatal().Err(err).Msg("unable to locate the Nomad Client Key")
			}

			// Export the TLS certificates path for Nomad Edge Deployer
			goos.Setenv(agent.NomadCACertEnvVarName, nomadConfig.NomadCACert)
			goos.Setenv(agent.NomadClientCertEnvVarName, nomadConfig.NomadClientCert)
			goos.Setenv(agent.NomadClientKeyEnvVarName, nomadConfig.NomadClientKey)
		}

		nomadConfig.NomadToken = goos.Getenv(agent.NomadTokenEnvVarName)

		log.Debug().
			Str("agent_port", options.AgentServerPort).
			Str("advertise_address", advertiseAddr).
			Str("NomadAddr", nomadConfig.NomadAddr).
			Msg("")
	}
	// !Nomad

	// Security
	signatureService := crypto.NewECDSAService(options.SharedSecret)

	if !options.EdgeMode {
		tlsService := crypto.TLSService{}

		err := tlsService.GenerateCertsForHost(advertiseAddr)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to generate self-signed certificates")
		}
	}

	// !Security

	if options.HealthCheck {
		err := healthcheck.Run(options, clusterService)
		if err != nil {
			log.Fatal().Err(err).Msg("failed healthcheck")
		}
		goos.Exit(0)
	}

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

		edgeKey, err := edge.RetrieveEdgeKey(options.EdgeKey, clusterService, options.DataPath)
		if err != nil {
			log.Error().Err(err).Msg("unable to retrieve Edge key")
		}

		if edgeKey != "" {
			log.Debug().Msg("edge key found in environment. Associating Edge key")

			err := edgeManager.SetKey(edgeKey)
			if err != nil {
				log.Fatal().Err(err).Msg("unable to associate Edge key")
			}

			err = edgeManager.Start()
			if err != nil {
				log.Fatal().Err(err).Msg("Unable to start Edge manager")
			}
		} else {
			log.Debug().Msg("edge key not specified. Serving Edge UI")
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
		log.Fatal().Err(err).Msg("unable to start registry server")
	}

	err = startAPIServer(config, options.EdgeMode)
	if err != nil && !errors.Is(err, gohttp.ErrServerClosed) {
		log.Fatal().Err(err).Msg("unable to start Agent API server")
	}

	// !API

	sigs := make(chan goos.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	s := <-sigs

	log.Debug().Stringer("signal", s).Msg("shutting down")
}

func startAPIServer(config *http.APIServerConfig, edgeMode bool) error {
	server := http.NewAPIServer(config)

	return server.Start(edgeMode)
}

func parseOptions() (*agent.Options, error) {
	optionParser := os.NewEnvOptionParser()
	return optionParser.Options()
}

func setLoggingLevel(level string) {
	switch level {
	case "ERROR":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "WARN":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "INFO":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "DEBUG":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
}

func serveEdgeUI(edgeManager *edge.Manager, serverAddr, serverPort string) {
	edgeServer := httpEdge.NewEdgeServer(edgeManager)

	go func() {
		log.Info().Str("server_address", serverAddr).Str("server_port", serverPort).Msg("Starting Edge UI server")

		err := edgeServer.Start(serverAddr, serverPort)
		if err != nil {
			log.Fatal().Err(err).Msg("Unable to start Edge server")
		}

		log.Info().Msg("Edge server shutdown")
	}()

	go func() {
		time.Sleep(agent.DefaultEdgeSecurityShutdown * time.Minute)

		if !edgeManager.IsKeySet() {
			log.Info().
				Int("shutdown_minutes", agent.DefaultEdgeSecurityShutdown).
				Msg("Shutting down Edge UI server as no key was specified after shutdown_minutes")

			edgeServer.Shutdown()
		}
	}()
}
