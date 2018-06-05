package handler

import (
	"log"
	"net/http"
	"os"
	"strings"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/proxy"
	"github.com/gorilla/mux"
)

// DockerProxyHandler represents an HTTP API handler for proxying requests to the Docker API.
type DockerProxyHandler struct {
	*mux.Router
	logger         *log.Logger
	dockerProxy    *proxy.SocketProxy
	clusterProxy   *proxy.ClusterProxy
	clusterService agent.ClusterService
	agentTags      map[string]string
}

// NewDockerProxyHandler returns a new instance of DockerProxyHandler.
// It sets the associated handle functions for all the Docker related HTTP endpoints.
func NewDockerProxyHandler(clusterService agent.ClusterService, agentTags map[string]string) *DockerProxyHandler {
	h := &DockerProxyHandler{
		Router:         mux.NewRouter(),
		logger:         log.New(os.Stderr, "", log.LstdFlags),
		dockerProxy:    proxy.NewSocketProxy("/var/run/docker.sock", clusterService),
		clusterProxy:   proxy.NewClusterProxy(),
		clusterService: clusterService,
		agentTags:      agentTags,
	}

	h.PathPrefix("/").Handler(http.HandlerFunc(h.handleDockerOperation))
	return h
}

func (handler *DockerProxyHandler) handleDockerOperation(rw http.ResponseWriter, request *http.Request) {
	managerOperationHeader := request.Header.Get(agent.HTTPManagerOperationHeaderName)

	if managerOperationHeader != "" {
		handler.executeOperationOnManagerNode(rw, request)
		return
	}

	handler.dispatchOperation(rw, request)
}

func (handler *DockerProxyHandler) dispatchOperation(rw http.ResponseWriter, request *http.Request) {
	path := request.URL.Path

	switch {
	case path == "/containers/json":
		handler.executeOperationOnCluster(rw, request)
		return
	case path == "/images/json":
		handler.executeOperationOnCluster(rw, request)
		return
	case path == "/volumes" && request.Method == http.MethodGet:
		handler.executeOperationOnCluster(rw, request)
		return
	case path == "/networks" && request.Method == http.MethodGet:
		handler.executeOperationOnCluster(rw, request)
		return
	case strings.HasPrefix(path, "/services"):
		handler.executeOperationOnManagerNode(rw, request)
		return
	case strings.HasPrefix(path, "/tasks"):
		handler.executeOperationOnManagerNode(rw, request)
		return
	case strings.HasPrefix(path, "/secrets"):
		handler.executeOperationOnManagerNode(rw, request)
		return
	case strings.HasPrefix(path, "/configs"):
		handler.executeOperationOnManagerNode(rw, request)
		return
	case strings.HasPrefix(path, "/swarm"):
		handler.executeOperationOnManagerNode(rw, request)
		return
	case strings.HasPrefix(path, "/nodes"):
		handler.executeOperationOnManagerNode(rw, request)
		return
	default:
		handler.executeOperationOnNode(rw, request)
	}
}

func (handler *DockerProxyHandler) executeOperationOnManagerNode(rw http.ResponseWriter, request *http.Request) {
	if handler.agentTags[agent.MemberTagKeyNodeRole] == agent.NodeRoleManager {
		handler.dockerProxy.ServeHTTP(rw, request)
	} else {
		targetMember := handler.clusterService.GetMemberByRole(agent.NodeRoleManager)
		if targetMember == nil {
			httperror.WriteErrorResponse(rw, agent.ErrManagerAgentNotFound, http.StatusInternalServerError, handler.logger)
			return
		}
		proxy.HTTPRequest(rw, request, targetMember)
	}
}

func (handler *DockerProxyHandler) executeOperationOnNode(rw http.ResponseWriter, request *http.Request) {
	agentTargetHeader := request.Header.Get(agent.HTTPTargetHeaderName)

	if agentTargetHeader == handler.agentTags[agent.MemberTagKeyNodeName] || agentTargetHeader == "" {
		handler.dockerProxy.ServeHTTP(rw, request)
	} else {
		targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
		if targetMember == nil {
			httperror.WriteErrorResponse(rw, agent.ErrAgentNotFound, http.StatusInternalServerError, handler.logger)
			return
		}

		proxy.HTTPRequest(rw, request, targetMember)
	}
}

func (handler *DockerProxyHandler) executeOperationOnCluster(rw http.ResponseWriter, request *http.Request) {
	agentTargetHeader := request.Header.Get(agent.HTTPTargetHeaderName)

	if agentTargetHeader == handler.agentTags[agent.MemberTagKeyNodeName] {
		handler.dockerProxy.ServeHTTP(rw, request)
	} else {
		clusterMembers := handler.clusterService.Members()

		data, err := handler.clusterProxy.ClusterOperation(request, clusterMembers)
		if err != nil {
			httperror.WriteErrorResponse(rw, err, http.StatusInternalServerError, handler.logger)
			return
		}

		writeJSONResponse(rw, data, handler.logger)
	}
}
