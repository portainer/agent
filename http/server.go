package http

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler"
)

// Server is the web server exposing the API of an agent.
type Server struct {
	addr             string
	port             string
	systemService    agent.SystemService
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	tunnelOperator   agent.TunnelOperator
	agentTags        map[string]string
	agentOptions     *agent.Options
}

// ServerConfig represents a server configuration
// used to create a new configuration
type ServerConfig struct {
	Addr             string
	Port             string
	SystemService    agent.SystemService
	ClusterService   agent.ClusterService
	SignatureService agent.DigitalSignatureService
	TunnelOperator   agent.TunnelOperator
	AgentTags        map[string]string
	AgentOptions     *agent.Options
	Secured          bool
}

// NewServer returns a pointer to a Server.
func NewServer(config *ServerConfig) *Server {
	return &Server{
		addr:             config.Addr,
		port:             config.Port,
		systemService:    config.SystemService,
		clusterService:   config.ClusterService,
		signatureService: config.SignatureService,
		tunnelOperator:   config.TunnelOperator,
		agentTags:        config.AgentTags,
		agentOptions:     config.AgentOptions,
	}
}

// Start starts a new web server by listening on the specified listenAddr.
// TODO: investigate potential security issues with running agent API in unsecured mode (container access in container network etc...)
// The agent API should not be exposed when running in unsecured mode. Still, a user could expose port 9000 and expose the Docker socket
// without any security.
func (server *Server) StartUnsecured() error {
	config := &handler.Config{
		SystemService:  server.systemService,
		ClusterService: server.clusterService,
		TunnelOperator: server.tunnelOperator,
		AgentTags:      server.agentTags,
		AgentOptions:   server.agentOptions,
		Secured:        false,
	}
	h := handler.NewHandler(config)

	// TODO: only use localhost:9001? this would prevent containers inside the same network to reach it?
	// See issue above
	listenAddr := server.addr + ":" + server.port
	return http.ListenAndServe(listenAddr, h)
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *Server) StartSecured() error {
	config := &handler.Config{
		SystemService:    server.systemService,
		ClusterService:   server.clusterService,
		SignatureService: server.signatureService,
		TunnelOperator:   server.tunnelOperator,
		AgentTags:        server.agentTags,
		AgentOptions:     server.agentOptions,
		Secured:          true,
	}
	h := handler.NewHandler(config)

	listenAddr := server.addr + ":" + server.port
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, h)
}
