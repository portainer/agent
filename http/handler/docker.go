package handler

import (
	"log"
	"net/http"
	"os"

	"bitbucket.org/portainer/agent"
	"bitbucket.org/portainer/agent/http/proxy"
	"github.com/gorilla/mux"
)

// DockerHandler represents an HTTP API handler for proxying requests to the Docker API.
type DockerHandler struct {
	*mux.Router
	logger *log.Logger
	proxy  *proxy.SocketProxy
}

// NewDockerHandler returns a new instance of DockerHandler.
func NewDockerHandler(clusterService agent.ClusterService) *DockerHandler {
	h := &DockerHandler{
		Router: mux.NewRouter(),
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}

	h.proxy = proxy.NewSocketProxy("/var/run/docker.sock", clusterService)

	h.PathPrefix("/").Handler(http.HandlerFunc(h.proxy.ServeHTTP))

	return h
}
