package http

import (
	"log"
	"net/http"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler"
)

// APIServer is the web server exposing the API of an agent.
type APIServer struct {
	addr             string
	port             string
	systemService    agent.SystemService
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	tunnelOperator   agent.TunnelOperator
	agentTags        map[string]string
	agentOptions     *agent.Options
	edgeMode         bool
	edgeStackManager agent.EdgeStackManager
}

// APIServerConfig represents a server configuration
// used to create a new API server
type APIServerConfig struct {
	Addr             string
	Port             string
	SystemService    agent.SystemService
	ClusterService   agent.ClusterService
	SignatureService agent.DigitalSignatureService
	TunnelOperator   agent.TunnelOperator
	AgentTags        map[string]string
	AgentOptions     *agent.Options
	EdgeMode         bool
	EdgeStackManager agent.EdgeStackManager
}

// NewAPIServer returns a pointer to a APIServer.
func NewAPIServer(config *APIServerConfig) *APIServer {
	return &APIServer{
		addr:             config.Addr,
		port:             config.Port,
		systemService:    config.SystemService,
		clusterService:   config.ClusterService,
		signatureService: config.SignatureService,
		tunnelOperator:   config.TunnelOperator,
		agentTags:        config.AgentTags,
		agentOptions:     config.AgentOptions,
		edgeMode:         config.EdgeMode,
		edgeStackManager: config.EdgeStackManager,
	}
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *APIServer) StartUnsecured() error {
	config := &handler.Config{
		SystemService:    server.systemService,
		ClusterService:   server.clusterService,
		TunnelOperator:   server.tunnelOperator,
		AgentTags:        server.agentTags,
		AgentOptions:     server.agentOptions,
		EdgeMode:         server.edgeMode,
		Secured:          false,
		EdgeStackManager: server.edgeStackManager,
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
		TunnelOperator:   server.tunnelOperator,
		AgentTags:        server.agentTags,
		AgentOptions:     server.agentOptions,
		EdgeMode:         server.edgeMode,
		Secured:          true,
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
