package handler

import (
	"log"
	"net/http"
	"os"
	"strings"

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
	h.Handle("/images/json", http.HandlerFunc(h.executeOperationAgainstCluster)).Methods(http.MethodGet)
	h.Handle("/volumes", http.HandlerFunc(h.executeOperationAgainstCluster)).Methods(http.MethodGet)
	h.Handle("/networks", http.HandlerFunc(h.executeOperationAgainstCluster)).Methods(http.MethodGet)
	h.PathPrefix("/services").Handler(http.HandlerFunc(h.executeOperationAgainstManagerNode))
	h.PathPrefix("/tasks").Handler(http.HandlerFunc(h.executeOperationAgainstManagerNode))
	h.PathPrefix("/secrets").Handler(http.HandlerFunc(h.executeOperationAgainstManagerNode))
	h.PathPrefix("/configs").Handler(http.HandlerFunc(h.executeOperationAgainstManagerNode))
	h.PathPrefix("/swarm").Handler(http.HandlerFunc(h.executeOperationAgainstManagerNode))
	h.PathPrefix("/nodes").Handler(http.HandlerFunc(h.executeOperationAgainstManagerNode))
	h.PathPrefix("/").Handler(http.HandlerFunc(h.executeOperationAgainstNode))

	return h
}

func (handler *DockerProxyHandler) executeOperationAgainstManagerNode(w http.ResponseWriter, request *http.Request) {
	agentOperationHeader := request.Header.Get(agent.HTTPOperationHeaderName)

	// TODO: if current node is on manager, passthrough?

	if agentOperationHeader == agent.HTTPOperationHeaderValue {
		handler.proxy.ServeHTTP(w, request)
	} else {

		targetMember := handler.clusterService.GetMemberByRole("manager")

		// TODO: find something to do when the targeted member is not found inside the cluster
		if targetMember == nil {
			httperror.WriteErrorResponse(w, agent.Error("Unable to find an agent located on a Swarm manager."),
				http.StatusInternalServerError, handler.logger)
		}

		// log.Printf("Redirecting request to approriate MANAGER node: %s\n\n", request.URL.String())
		operations.NodeOperation2(w, request, targetMember)
		// response, err := operations.NodeOperation(request, targetMember)
		// if err != nil {
		// 	httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
		// }
		//
		// err = copyResponseToResponseWriter(response, w)
		// if err != nil {
		// 	httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
		// }
	}
}

func (handler *DockerProxyHandler) executeOperationAgainstNode(w http.ResponseWriter, request *http.Request) {

	agentOperationHeader := request.Header.Get(agent.HTTPOperationHeaderName)
	agentTargetHeader := request.Header.Get(agent.HTTPTargetHeaderName)

	if agentOperationHeader == agent.HTTPOperationHeaderValue || agentTargetHeader == "" {
		handler.proxy.ServeHTTP(w, request)
	} else {
		targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
		// TODO: find something to do when the targeted member is not found inside the cluster
		if targetMember == nil {
			httperror.WriteErrorResponse(w, agent.Error("Unable to find the agent."),
				http.StatusInternalServerError, handler.logger)
		}

		// log.Printf("Redirecting request to approriate node: %s\n\n", request.URL.String())
		operations.NodeOperation2(w, request, targetMember)
		// response, err := operations.NodeOperation(request, targetMember)
		// if err != nil {
		// 	httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
		// }
		//
		// err = copyResponseToResponseWriter(response, w)
		// if err != nil {
		// 	httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
		// }
	}
}

func (handler *DockerProxyHandler) executeOperationAgainstCluster(w http.ResponseWriter, request *http.Request) {

	// log.Printf("Cluster operation for: %s\n\n", request.URL.String())

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
		if strings.HasPrefix(request.URL.Path, "/volumes") {
			responseObject := make(map[string]interface{})
			responseObject["Volumes"] = data
			encodeJSON(w, responseObject, handler.logger)
		} else {
			encodeJSON(w, data, handler.logger)
		}
	}
}
