package docker

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) dockerOperation(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	if handler.clusterService == nil {
		handler.dockerProxy.ServeHTTP(rw, request)
		return nil
	}

	managerOperationHeader := request.Header.Get(agent.HTTPManagerOperationHeaderName)

	if managerOperationHeader != "" {
		return handler.executeOperationOnManagerNode(rw, request)
	}

	return handler.dispatchOperation(rw, request)
}

func (handler *Handler) dispatchOperation(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	path := request.URL.Path

	switch {
	case path == "/containers/json":
		return handler.executeOperationOnCluster(rw, request)
	case path == "/images/json":
		return handler.executeOperationOnCluster(rw, request)
	case path == "/volumes" && request.Method == http.MethodGet:
		return handler.executeOperationOnCluster(rw, request)
	case path == "/networks" && request.Method == http.MethodGet:
		return handler.executeOperationOnCluster(rw, request)
	case strings.HasPrefix(path, "/services"):
		return handler.executeOperationOnManagerNode(rw, request)
	case strings.HasPrefix(path, "/tasks"):
		return handler.executeOperationOnManagerNode(rw, request)
	case strings.HasPrefix(path, "/secrets"):
		return handler.executeOperationOnManagerNode(rw, request)
	case strings.HasPrefix(path, "/configs"):
		return handler.executeOperationOnManagerNode(rw, request)
	case strings.HasPrefix(path, "/swarm"):
		return handler.executeOperationOnManagerNode(rw, request)
	case strings.HasPrefix(path, "/info"):
		return handler.executeOperationOnManagerNode(rw, request)
	case strings.HasPrefix(path, "/nodes"):
		return handler.executeOperationOnManagerNode(rw, request)
	default:
		return handler.executeOperationOnNode(rw, request)
	}
}

func (handler *Handler) executeOperationOnManagerNode(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	if handler.runtimeConfiguration.DockerConfiguration.NodeRole == agent.NodeRoleManager {
		handler.dockerProxy.ServeHTTP(rw, request)
	} else {
		targetMember := handler.clusterService.GetMemberByRole(agent.NodeRoleManager)
		if targetMember == nil {
			log.Printf("[ERROR] [http,docker,proxy] [request: %s] [message: unable to redirect request to a manager node: no manager node found]", request.URL)
			return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent located on a manager node", errors.New("Unable to find an agent on any manager node")}
		}
		proxy.AgentHTTPRequest(rw, request, targetMember, handler.useTLS)
	}
	return nil
}

func (handler *Handler) executeOperationOnNode(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	agentTargetHeader := request.Header.Get(agent.HTTPTargetHeaderName)

	if agentTargetHeader == handler.runtimeConfiguration.NodeName || agentTargetHeader == "" {
		handler.dockerProxy.ServeHTTP(rw, request)
	} else {
		targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
		if targetMember == nil {
			log.Printf("[ERROR] [http,docker,proxy] [target_node: %s] [request: %s] [message: unable to redirect request to specified node: agent not found in cluster]", agentTargetHeader, request.URL)
			return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent", errors.New("Unable to find the targeted agent")}
		}

		proxy.AgentHTTPRequest(rw, request, targetMember, handler.useTLS)
	}
	return nil
}

func (handler *Handler) executeOperationOnCluster(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	agentTargetHeader := request.Header.Get(agent.HTTPTargetHeaderName)

	if agentTargetHeader == handler.runtimeConfiguration.NodeName {
		handler.dockerProxy.ServeHTTP(rw, request)
		return nil
	}

	clusterMembers := handler.clusterService.Members()

	data, err := handler.clusterProxy.ClusterOperation(request, clusterMembers)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to execute cluster operation", err}
	}

	return response.JSON(rw, data)
}
