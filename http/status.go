package http

import (
	"net/http"
	"time"

	"github.com/portainer/agent"

	"github.com/gorilla/mux"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

// StatusServer expose an API to retrieve the status of the agent
type StatusServer struct {
	addr           string
	port           string
	clusterService agent.ClusterService
	edgeMode       bool
}

type StatusServerConfig struct {
	Addr           string
	Port           string
	ClusterService agent.ClusterService
	EdgeMode       bool
}

// NewStatusServer returns a pointer to a new instance of StatusServer.
func NewStatusServer(config *StatusServerConfig) *StatusServer {
	return &StatusServer{
		addr:           config.Addr,
		port:           config.Port,
		clusterService: config.ClusterService,
		edgeMode:       config.EdgeMode,
	}
}

// Start starts a new web server by listening on the specified addr and port.
func (server *StatusServer) Start() error {
	router := mux.NewRouter()
	router.Handle("/status", httperror.LoggerHandler(server.statusInspect))

	listenAddr := server.addr + ":" + server.port
	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      router,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	return httpServer.ListenAndServe()
}

type (
	clusterStatus struct {
		Members []agent.ClusterMember `json:"members"`
	}

	edgeStatus struct {
		Enabled bool `json:"enabled"`
	}

	statusInspectResponse struct {
		ClusterStatus clusterStatus `json:"cluster"`
		Edge          edgeStatus    `json:"edge"`
		NodeName      string        `json:"nodeName"`
	}
)

func (server *StatusServer) statusInspect(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	resp := statusInspectResponse{
		ClusterStatus: clusterStatus{},
		Edge: edgeStatus{
			Enabled: server.edgeMode,
		},
	}

	members := server.clusterService.Members()
	resp.ClusterStatus.Members = members

	tags := server.clusterService.GetTags()
	resp.NodeName = tags[agent.MemberTagKeyNodeName]

	return response.JSON(w, resp)
}
