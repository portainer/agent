package http

import (
	"net/http"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler"
)

// Server is the web server exposing the API of an agent.
type Server struct {
	systemService    agent.SystemService
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	agentTags        map[string]string
	agentOptions     *agent.AgentOptions
}

// NewServer returns a pointer to a Server.
func NewServer(systemService agent.SystemService, clusterService agent.ClusterService, signatureService agent.DigitalSignatureService, agentTags map[string]string, agentOptions *agent.AgentOptions) *Server {
	return &Server{
		systemService:    systemService,
		clusterService:   clusterService,
		signatureService: signatureService,
		agentTags:        agentTags,
		agentOptions:     agentOptions,
	}
}

// Start starts a new webserver by listening on the specified listenAddr.
func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.systemService, server.clusterService, server.signatureService, server.agentTags, server.agentOptions)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, http.TimeoutHandler(h, 5*time.Second, ""))
}
