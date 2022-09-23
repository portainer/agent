package edge

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/revoke"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	portainer "github.com/portainer/portainer/api"

	"github.com/rs/zerolog/log"
)

type (
	// Manager is used to manage all Edge features through multiple sub-components. It is mainly responsible for running the Edge background process.
	Manager struct {
		containerPlatform agent.ContainerPlatform
		advertiseAddr     string
		agentOptions      *agent.Options
		clusterService    agent.ClusterService
		dockerInfoService agent.DockerInfoService
		key               *edgeKey
		logsManager       *scheduler.LogsManager
		pollService       *PollService
		stackManager      *stack.StackManager
		mu                sync.Mutex
	}

	// ManagerParameters represents an object used to create a Manager
	ManagerParameters struct {
		Options           *agent.Options
		AdvertiseAddr     string
		ClusterService    agent.ClusterService
		DockerInfoService agent.DockerInfoService
		ContainerPlatform agent.ContainerPlatform
	}
)

func (manager *Manager) GetStackManager() *stack.StackManager {
	return manager.stackManager
}

// NewManager returns a pointer to a new instance of Manager
func NewManager(parameters *ManagerParameters) *Manager {
	return &Manager{
		clusterService:    parameters.ClusterService,
		dockerInfoService: parameters.DockerInfoService,
		agentOptions:      parameters.Options,
		advertiseAddr:     parameters.AdvertiseAddr,
		containerPlatform: parameters.ContainerPlatform,
	}
}

// Start starts the manager
func (manager *Manager) Start() error {
	if !manager.IsKeySet() {
		return errors.New("unable to Start Edge manager without key")
	}

	apiServerAddr := fmt.Sprintf("%s:%s", manager.advertiseAddr, manager.agentOptions.AgentServerPort)

	pollServiceConfig := &pollServiceConfig{
		APIServerAddr:           apiServerAddr,
		EdgeID:                  manager.agentOptions.EdgeID,
		PollFrequency:           agent.DefaultEdgePollInterval,
		InactivityTimeout:       manager.agentOptions.EdgeInactivityTimeout,
		TunnelCapability:        manager.agentOptions.EdgeTunnel,
		PortainerURL:            manager.key.PortainerInstanceURL,
		TunnelServerAddr:        manager.key.TunnelServerAddr,
		TunnelServerFingerprint: manager.key.TunnelServerFingerprint,
		ContainerPlatform:       manager.containerPlatform,
	}

	log.Debug().
		Str("api_addr", pollServiceConfig.APIServerAddr).
		Str("edge_id", pollServiceConfig.EdgeID).
		Str("poll_frequency", pollServiceConfig.PollFrequency).
		Str("inactivity_timeout", pollServiceConfig.InactivityTimeout).
		Bool("insecure_poll", manager.agentOptions.EdgeInsecurePoll).
		Bool("tunnel_capability", manager.agentOptions.EdgeTunnel).
		Msg("")

	// When the header is not set to PlatformDocker Portainer assumes the platform to be kubernetes.
	// However, Portainer should handle podman agents the same way as docker agents.
	agentPlatform := manager.containerPlatform
	if manager.containerPlatform == agent.PlatformPodman {
		agentPlatform = agent.PlatformDocker
	}

	portainerClient := client.NewPortainerClient(
		manager.key.PortainerInstanceURL,
		manager.SetEndpointID,
		manager.GetEndpointID,
		manager.agentOptions.EdgeID,
		manager.agentOptions.EdgeAsyncMode,
		agentPlatform,
		client.BuildHTTPClient(10, manager.agentOptions),
	)

	manager.stackManager = stack.NewStackManager(
		portainerClient,
		manager.agentOptions.AssetsPath,
	)

	manager.logsManager = scheduler.NewLogsManager(portainerClient)
	manager.logsManager.Start()

	pollService, err := newPollService(
		manager,
		manager.stackManager,
		manager.logsManager,
		pollServiceConfig,
		portainerClient,
		manager.agentOptions.EdgeAsyncMode,
	)
	if err != nil {
		return err
	}
	manager.pollService = pollService

	return manager.startEdgeBackgroundProcess()
}

// ResetActivityTimer resets the activity timer
func (manager *Manager) ResetActivityTimer() {
	manager.pollService.resetActivityTimer()
}

// SetEndpointID set the endpointID of the agent
func (manager *Manager) SetEndpointID(endpointID portainer.EndpointID) {
	manager.mu.Lock()
	if manager.key.EndpointID != endpointID {
		log.Info().Int("endpoint_id", int(endpointID)).Msg("setting endpoint ID")

		manager.key.EndpointID = endpointID
	}
	manager.mu.Unlock()
}

// GetEndpointID gets the endpointID of the agent
func (manager *Manager) GetEndpointID() portainer.EndpointID {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	return manager.key.EndpointID
}

func (manager *Manager) startEdgeBackgroundProcessOnDocker(runtimeCheckFrequency time.Duration) error {
	err := manager.checkDockerRuntimeConfig()
	if err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			err := manager.checkDockerRuntimeConfig()
			if err != nil {
				log.Error().Msg("an error occurred during Docker runtime configuration check")
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcessOnKubernetes(runtimeCheckFrequency time.Duration) error {
	manager.pollService.Start()

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			manager.pollService.Start()

			err := manager.stackManager.SetEngineStatus(stack.EngineTypeKubernetes)
			if err != nil {
				log.Error().Err(err).Msg("unable to set engine status")

				return
			}

			err = manager.stackManager.Start()
			if err != nil {
				log.Error().Err(err).Msg("unable to Start stack manager")

				return
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcessOnNomad(runtimeCheckFrequency time.Duration) error {
	manager.pollService.Start()

	go func() {
		ticker := time.NewTicker(runtimeCheckFrequency)
		for range ticker.C {
			manager.pollService.Start()

			err := manager.stackManager.SetEngineStatus(stack.EngineTypeNomad)
			if err != nil {
				log.Error().Err(err).Msg("unable to set engine status")

				return
			}

			err = manager.stackManager.Start()
			if err != nil {
				log.Error().Err(err).Msg("unable to start stack manager")

				return
			}
		}
	}()

	return nil
}

func (manager *Manager) startEdgeBackgroundProcess() error {
	runtimeCheckFrequency, err := time.ParseDuration(agent.DefaultConfigCheckInterval)
	if err != nil {
		return err
	}

	switch manager.containerPlatform {
	case agent.PlatformDocker:
		return manager.startEdgeBackgroundProcessOnDocker(runtimeCheckFrequency)
	case agent.PlatformKubernetes:
		return manager.startEdgeBackgroundProcessOnKubernetes(runtimeCheckFrequency)
	case agent.PlatformNomad:
		return manager.startEdgeBackgroundProcessOnNomad(runtimeCheckFrequency)
	}

	return nil
}

func (manager *Manager) checkDockerRuntimeConfig() error {
	runtimeConfiguration, err := manager.dockerInfoService.GetRuntimeConfigurationFromDockerEngine()
	if err != nil {
		return err
	}

	agentRunsOnLeaderNode := runtimeConfiguration.DockerConfiguration.Leader
	agentRunsOnSwarm := runtimeConfiguration.DockerConfiguration.EngineStatus == agent.EngineStatusSwarm

	log.Debug().
		Str("engine_status", fmt.Sprintf("%+v", runtimeConfiguration.DockerConfiguration.EngineStatus)).
		Bool("leader_node", agentRunsOnLeaderNode).
		Msg("Docker runtime configuration check")

	if !agentRunsOnSwarm || agentRunsOnLeaderNode {
		engineStatus := stack.EngineTypeDockerStandalone
		if agentRunsOnSwarm {
			engineStatus = stack.EngineTypeDockerSwarm
		}

		manager.pollService.Start()

		err = manager.stackManager.SetEngineStatus(engineStatus)
		if err != nil {
			return err
		}

		return manager.stackManager.Start()
	}

	manager.pollService.Stop()

	return manager.stackManager.Stop()
}

func buildHTTPClient(timeout float64, options *agent.Options) *http.Client {
	return &http.Client{
		Transport: buildTransport(options),
		Timeout:   time.Duration(timeout) * time.Second,
	}
}

func buildTransport(options *agent.Options) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	transport.TLSClientConfig = &tls.Config{
		ClientSessionCache: tls.NewLRUClientSessionCache(0),
		MinVersion:         tls.VersionTLS12,
		CipherSuites:       crypto.TLS12CipherSuites,
	}

	if options.EdgeInsecurePoll {
		transport.TLSClientConfig.InsecureSkipVerify = true
		return transport
	}

	if options.SSLCert != "" && options.SSLKey != "" {
		revokeService := revoke.NewService()

		// Create a CA certificate pool and add cert.pem to it
		var caCertPool *x509.CertPool
		if options.SSLCACert != "" {
			caCert, err := ioutil.ReadFile(options.SSLCACert)
			if err != nil {
				log.Fatal().Err(err).Msg("")
			}

			caCertPool = x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
		}

		transport.TLSClientConfig.RootCAs = caCertPool
		transport.TLSClientConfig.GetClientCertificate = func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(options.SSLCert, options.SSLKey)

			return &cert, err
		}

		transport.TLSClientConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			for _, chain := range verifiedChains {
				for _, cert := range chain {
					revoked, err := revokeService.VerifyCertificate(cert)
					if err != nil {
						return err
					}

					if revoked {
						return errors.New("certificate has been revoked")
					}
				}
			}

			return nil
		}

	}

	return transport
}
