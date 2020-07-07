package http

import (
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
	addr             string
	port             string
	systemService    agent.SystemService
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	edgeManager      *edge.Manager
	agentTags        *agent.RuntimeConfiguration
	agentOptions     *agent.Options
	kubeClient       *kubernetes.KubeClient
}

// APIServerConfig represents a server configuration
// used to create a new API server
type APIServerConfig struct {
	Addr             string
	Port             string
	SystemService    agent.SystemService
	ClusterService   agent.ClusterService
	SignatureService agent.DigitalSignatureService
	EdgeManager      *edge.Manager
	KubeClient       *kubernetes.KubeClient
	AgentTags        *agent.RuntimeConfiguration
	AgentOptions     *agent.Options
}

// NewAPIServer returns a pointer to a APIServer.
func NewAPIServer(config *APIServerConfig) *APIServer {
	return &APIServer{
		addr:             config.Addr,
		port:             config.Port,
		systemService:    config.SystemService,
		clusterService:   config.ClusterService,
		signatureService: config.SignatureService,
		edgeManager:      config.EdgeManager,
		agentTags:        config.AgentTags,
		agentOptions:     config.AgentOptions,
		kubeClient:       config.KubeClient,
	}
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *APIServer) StartUnsecured() error {
	config := &handler.Config{
		SystemService:  server.systemService,
		ClusterService: server.clusterService,
		AgentTags:      server.agentTags,
		AgentOptions:   server.agentOptions,
		EdgeManager:    server.edgeManager,
		Secured:        false,
		KubeClient:     server.kubeClient,
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
		SystemService:    server.systemService,
		ClusterService:   server.clusterService,
		SignatureService: server.signatureService,
		AgentTags:        server.agentTags,
		AgentOptions:     server.agentOptions,
		EdgeManager:      server.edgeManager,
		Secured:          true,
		KubeClient:       server.kubeClient,
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

	return httpServer.ListenAndServeTLS(agent.TLSCertPath, agent.TLSKeyPath)
}
