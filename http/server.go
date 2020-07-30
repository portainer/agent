package http

import (
	"crypto/tls"
	"log"
	"net/http"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler"
	"github.com/portainer/agent/internal/edge"
	"github.com/portainer/agent/kubernetes"
)

// APIServer is the web server exposing the API of an agent.
type APIServer struct {
	addr              string
	port              string
	systemService     agent.SystemService
	clusterService    agent.ClusterService
	signatureService  agent.DigitalSignatureService
	edgeManager       *edge.Manager
	agentTags         *agent.RuntimeConfiguration
	agentOptions      *agent.Options
	kubeClient        *kubernetes.KubeClient
	containerPlatform agent.ContainerPlatform
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
	RuntimeConfiguration *agent.RuntimeConfiguration
	AgentOptions         *agent.Options
	ContainerPlatform    agent.ContainerPlatform
}

// NewAPIServer returns a pointer to a APIServer.
func NewAPIServer(config *APIServerConfig) *APIServer {
	return &APIServer{
		addr:              config.Addr,
		port:              config.Port,
		systemService:     config.SystemService,
		clusterService:    config.ClusterService,
		signatureService:  config.SignatureService,
		edgeManager:       config.EdgeManager,
		agentTags:         config.RuntimeConfiguration,
		agentOptions:      config.AgentOptions,
		kubeClient:        config.KubeClient,
		containerPlatform: config.ContainerPlatform,
	}
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *APIServer) StartUnsecured() error {
	config := &handler.Config{
		SystemService:        server.systemService,
		ClusterService:       server.clusterService,
		RuntimeConfiguration: server.agentTags,
		AgentOptions:         server.agentOptions,
		EdgeManager:          server.edgeManager,
		Secured:              false,
		KubeClient:           server.kubeClient,
		ContainerPlatform:    server.containerPlatform,
	}

	h := handler.NewHandler(config)
	listenAddr := server.addr + ":" + server.port

	log.Printf("[INFO] [http] [server_addr: %s] [server_port: %s] [secured: %t] [api_version: %s] [message: Starting Agent API server]", server.addr, server.port, config.Secured, agent.Version)

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      h,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	return httpServer.ListenAndServe()
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *APIServer) StartSecured() error {
	config := &handler.Config{
		SystemService:        server.systemService,
		ClusterService:       server.clusterService,
		SignatureService:     server.signatureService,
		RuntimeConfiguration: server.agentTags,
		AgentOptions:         server.agentOptions,
		EdgeManager:          server.edgeManager,
		Secured:              true,
		KubeClient:           server.kubeClient,
		ContainerPlatform:    server.containerPlatform,
	}

	h := handler.NewHandler(config)
	listenAddr := server.addr + ":" + server.port

	log.Printf("[INFO] [http] [server_addr: %s] [server_port: %s] [secured: %t] [api_version: %s] [message: Starting Agent API server]", server.addr, server.port, config.Secured, agent.Version)

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
	}

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      h,
		ReadTimeout:  5 * time.Second,
		TLSConfig:    tlsConfig,
		WriteTimeout: 120 * time.Second,
	}

	return httpServer.ListenAndServeTLS(agent.TLSCertPath, agent.TLSKeyPath)
}
