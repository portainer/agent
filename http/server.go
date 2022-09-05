package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/edge"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/http/handler"
	"github.com/portainer/agent/kubernetes"
	httpError "github.com/portainer/libhttp/error"

	"github.com/rs/zerolog/log"
)

// APIServer is the web server exposing the API of an agent.
type APIServer struct {
	addr               string
	port               string
	systemService      agent.SystemService
	clusterService     agent.ClusterService
	signatureService   agent.DigitalSignatureService
	edgeManager        *edge.Manager
	agentTags          *agent.RuntimeConfiguration
	agentOptions       *agent.Options
	kubeClient         *kubernetes.KubeClient
	kubernetesDeployer *exec.KubernetesDeployer
	containerPlatform  agent.ContainerPlatform
	nomadConfig        agent.NomadConfig
}

// APIServerConfig represents a server configuration
// used to create a new API server
type APIServerConfig struct {
	Addr                 string
	Port                 string
	SystemService        agent.SystemService
	ClusterService       agent.ClusterService
	SignatureService     agent.DigitalSignatureService
	EdgeManager          *edge.Manager
	KubeClient           *kubernetes.KubeClient
	KubernetesDeployer   *exec.KubernetesDeployer
	RuntimeConfiguration *agent.RuntimeConfiguration
	AgentOptions         *agent.Options
	ContainerPlatform    agent.ContainerPlatform
	NomadConfig          agent.NomadConfig
}

// NewAPIServer returns a pointer to a APIServer.
func NewAPIServer(config *APIServerConfig) *APIServer {
	return &APIServer{
		addr:               config.Addr,
		port:               config.Port,
		systemService:      config.SystemService,
		clusterService:     config.ClusterService,
		signatureService:   config.SignatureService,
		edgeManager:        config.EdgeManager,
		agentTags:          config.RuntimeConfiguration,
		agentOptions:       config.AgentOptions,
		kubeClient:         config.KubeClient,
		kubernetesDeployer: config.KubernetesDeployer,
		containerPlatform:  config.ContainerPlatform,
		nomadConfig:        config.NomadConfig,
	}
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *APIServer) Start(edgeMode bool) error {
	config := &handler.Config{
		SystemService:        server.systemService,
		ClusterService:       server.clusterService,
		SignatureService:     server.signatureService,
		RuntimeConfiguration: server.agentTags,
		EdgeManager:          server.edgeManager,
		KubeClient:           server.kubeClient,
		KubernetesDeployer:   server.kubernetesDeployer,
		UseTLS:               !edgeMode,
		ContainerPlatform:    server.containerPlatform,
		NomadConfig:          server.nomadConfig,
	}

	httpHandler := handler.NewHandler(config)
	httpServer := &http.Server{
		Addr:         server.addr + ":" + server.port,
		Handler:      httpHandler,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 30 * time.Minute,
	}

	log.Info().
		Str("server_addr", server.addr).
		Str("server_port", server.port).
		Bool("use_tls", config.UseTLS).
		Str("api_version", agent.Version).
		Msg("starting Agent API server")

	if edgeMode {
		httpServer.Handler = server.edgeHandler(httpHandler)
		return httpServer.ListenAndServe()
	}

	httpServer.TLSConfig = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: crypto.TLS12CipherSuites,
	}

	go server.securityShutdown(httpServer)

	return httpServer.ListenAndServeTLS(agent.TLSCertPath, agent.TLSKeyPath)
}

func (server *APIServer) securityShutdown(httpServer *http.Server) {
	time.Sleep(server.agentOptions.AgentSecurityShutdown)

	if server.signatureService.IsAssociated() {
		return
	}

	log.Info().
		Dur("timeout", server.agentOptions.AgentSecurityShutdown).
		Msg("shutting down API server as no client was associated after the timeout, keeping alive to prevent restart by docker/kubernetes")

	err := httpServer.Shutdown(context.Background())
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("failed shutting down server")
	}
}

func (server *APIServer) edgeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !server.edgeManager.IsKeySet() {
			httpError.WriteError(w, http.StatusForbidden, "Unable to use the unsecured agent API without Edge key", errors.New("edge key not set"))
			return
		}

		server.edgeManager.ResetActivityTimer()

		next.ServeHTTP(w, r)
	})
}
