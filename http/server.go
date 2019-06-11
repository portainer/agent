package http

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler"
)

// Server is the web server exposing the API of an agent.
type Server struct {
	systemService    agent.SystemService
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	agentTags        map[string]string
	agentOptions     *agent.Options
}

// NewServer returns a pointer to a Server.
func NewServer(systemService agent.SystemService, clusterService agent.ClusterService, signatureService agent.DigitalSignatureService, agentTags map[string]string, agentOptions *agent.Options) *Server {
	return &Server{
		systemService:    systemService,
		clusterService:   clusterService,
		signatureService: signatureService,
		agentTags:        agentTags,
		agentOptions:     agentOptions,
	}
}

// Start starts a new web server by listening on the specified listenAddr.
func (server *Server) Start(addr, port string) error {
	h := handler.NewHandler(server.systemService, server.clusterService, server.signatureService, server.agentTags, server.agentOptions)

	// TODO: better management of HTTP/HTTPS as the agent must be able to talk with the other agents
	// using the same protocol
	// if started in edge, all requests to other agents must use http/ws
	// if not, use https/wss

	listenAddr := addr + ":" + port
	if server.agentOptions.EdgeMode {
		return http.ListenAndServe(listenAddr, h)
	}
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, h)
}
