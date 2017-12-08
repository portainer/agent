package http

import (
	"net/http"

	"bitbucket.org/portainer/agent"
	"bitbucket.org/portainer/agent/http/handler"
)

type Server struct {
	ClusterService agent.ClusterService
}

func (server *Server) Start(listenAddr string) error {

	h := handler.NewHandler(server.ClusterService)

	return http.ListenAndServe(listenAddr, h)
}
