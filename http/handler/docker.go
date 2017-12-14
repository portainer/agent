package handler

import (
	"log"
	"net/http"
	"os"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/operations"
	"bitbucket.org/portainer/agent/http/proxy"
	"github.com/gorilla/mux"
)

// DockerProxyHandler represents an HTTP API handler for proxying requests to the Docker API.
type DockerProxyHandler struct {
	*mux.Router
	logger         *log.Logger
	proxy          *proxy.SocketProxy
	clusterService agent.ClusterService
}

// NewDockerProxyHandler returns a new instance of DockerProxyHandler.
func NewDockerProxyHandler(clusterService agent.ClusterService) *DockerProxyHandler {
	h := &DockerProxyHandler{
		Router:         mux.NewRouter(),
		logger:         log.New(os.Stderr, "", log.LstdFlags),
		proxy:          proxy.NewSocketProxy("/var/run/docker.sock", clusterService),
		clusterService: clusterService,
	}

	h.Handle("/containers/json", http.HandlerFunc(h.executeOperationAgainstCluster)).Methods(http.MethodGet)
	h.PathPrefix("/").Handler(http.HandlerFunc(h.executeOperationAgainstNode))

	return h
}

func (handler *DockerProxyHandler) executeOperationAgainstNode(w http.ResponseWriter, request *http.Request) {
	agentOperationHeader := request.Header.Get(agent.HTTPOperationHeaderName)
	agentTargetHeader := request.Header.Get(agent.HTTPTargetHeaderName)
	if agentOperationHeader == agent.HTTPOperationHeaderValue || agentTargetHeader == "" {
		handler.proxy.ServeHTTP(w, request)
	} else {

		// TODO: finding the member should probably be relocated somewhere else.
		// Also we might want to check if the member is alive.
		var targetMember *agent.ClusterMember
		members := handler.clusterService.Members()
		for _, member := range members {
			if member.Name == agentTargetHeader {
				targetMember = &member
				break
			}
		}

		// TODO: find something to do when the targeted member is not found inside the cluster
		if targetMember == nil {
			httperror.WriteErrorResponse(w, agent.Error("Member not found!"), http.StatusInternalServerError, handler.logger)
		}

		data, err := operations.NodeOperation(request, targetMember)
		if err != nil {
			httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
		}

		// TODO: Integrate all the headers available in a regular request to the Docker API (docker API version, experimental...).
		// Example response header from the Docker API:
		// Api-Version: 1.32
		// Content-Length: 1352
		// Content-Type: application/json
		// Date: Tue, 12 Dec 2017 11:24:33 GMT
		// Docker-Experimental: false
		// Ostype: linux
		// Server: Docker/17.09.1-ce (linux)
		encodeJSON(w, data, handler.logger)
	}
}

func (handler *DockerProxyHandler) executeOperationAgainstCluster(w http.ResponseWriter, request *http.Request) {

	agentOperationHeader := request.Header.Get(agent.HTTPOperationHeaderName)
	if agentOperationHeader == agent.HTTPOperationHeaderValue {
		handler.proxy.ServeHTTP(w, request)
	} else {
		clusterMembers := handler.clusterService.Members()

		data, err := operations.ClusterOperation(request, clusterMembers)
		if err != nil {
			httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
		}

		// TODO: Integrate all the headers available in a regular request to the Docker API (docker API version, experimental...).
		// Example response header from the Docker API:
		// Api-Version: 1.32
		// Content-Length: 1352
		// Content-Type: application/json
		// Date: Tue, 12 Dec 2017 11:24:33 GMT
		// Docker-Experimental: false
		// Ostype: linux
		// Server: Docker/17.09.1-ce (linux)
		encodeJSON(w, data, handler.logger)
	}
}
