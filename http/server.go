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
	notaryService    agent.NotaryService
	agentTags        map[string]string
}

// NewServer returns a pointer to a Server.
func NewServer(systemService agent.SystemService, clusterService agent.ClusterService, signatureService agent.DigitalSignatureService, notaryService agent.NotaryService, agentTags map[string]string) *Server {
	return &Server{
		systemService:    systemService,
		clusterService:   clusterService,
		signatureService: signatureService,
		notaryService:    notaryService,
		agentTags:        agentTags,
	}
}

// Start starts a new webserver by listening on the specified listenAddr.
func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.systemService, server.clusterService, server.notaryService, server.agentTags)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, h)
}
