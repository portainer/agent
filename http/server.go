package http

import (
	"net/http"

	"bitbucket.org/portainer/agent"
	"bitbucket.org/portainer/agent/http/handler"
)

func NewServer(clusterService agent.ClusterService, agentTags map[string]string) *Server {
	return &Server{
		clusterService: clusterService,
		agentTags:      agentTags,
	}
}

type Server struct {
	clusterService agent.ClusterService
	agentTags      map[string]string
}

func (server *Server) Start(listenAddr string) error {

	h := handler.NewHandler(server.clusterService, server.agentTags)
	return http.ListenAndServe(listenAddr, h)
}
