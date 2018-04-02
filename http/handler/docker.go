package handler

import (
	"log"
	"net/http"
	"os"

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
func NewDockerProxyHandler(clusterService agent.ClusterService, agentTags map[string]string) *DockerProxyHandler {
	h := &DockerProxyHandler{
		Router:         mux.NewRouter(),
		logger:         log.New(os.Stderr, "", log.LstdFlags),
		dockerProxy:    proxy.NewSocketProxy("/var/run/docker.sock", clusterService),
		clusterProxy:   proxy.NewClusterProxy(),
		clusterService: clusterService,
		agentTags:      agentTags,
	}

	h.Handle("/containers/json", http.HandlerFunc(h.executeOperationOnCluster)).Methods(http.MethodGet)
	h.Handle("/images/json", http.HandlerFunc(h.executeOperationOnCluster)).Methods(http.MethodGet)
	h.Handle("/volumes", http.HandlerFunc(h.executeOperationOnCluster)).Methods(http.MethodGet)
	h.Handle("/networks", http.HandlerFunc(h.executeOperationOnCluster)).Methods(http.MethodGet)
	h.PathPrefix("/services").Handler(http.HandlerFunc(h.executeOperationOnManagerNode))
	h.PathPrefix("/tasks").Handler(http.HandlerFunc(h.executeOperationOnManagerNode))
	h.PathPrefix("/secrets").Handler(http.HandlerFunc(h.executeOperationOnManagerNode))
	h.PathPrefix("/configs").Handler(http.HandlerFunc(h.executeOperationOnManagerNode))
	h.PathPrefix("/swarm").Handler(http.HandlerFunc(h.executeOperationOnManagerNode))
	h.PathPrefix("/nodes").Handler(http.HandlerFunc(h.executeOperationOnManagerNode))
	h.PathPrefix("/").Handler(http.HandlerFunc(h.executeOperationOnNode))

	return h
}

func (handler *DockerProxyHandler) executeOperationOnManagerNode(rw http.ResponseWriter, request *http.Request) {
	if handler.agentTags[agent.MemberTagKeyNodeRole] == agent.NodeRoleManager {
		handler.dockerProxy.ServeHTTP(rw, request)
	} else {
		targetMember := handler.clusterService.GetMemberByRole("zob")
		if targetMember == nil {
			httperror.WriteErrorResponse(rw, agent.ErrManagerAgentNotFound, http.StatusInternalServerError, handler.logger)
			return
		}
		proxy.ProxyOperation(rw, request, targetMember)
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

		proxy.ProxyOperation(rw, request, targetMember)
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
