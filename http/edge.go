package http

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/portainer/agent/http/client"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
)

// EdgeServer expose an UI to associate an Edge key with the agent.
type EdgeServer struct {
	httpServer     *http.Server
	tunnelOperator agent.TunnelOperator
	clusterService agent.ClusterService
	keyService     agent.EdgeKeyService
}

// NewEdgeServer returns a pointer to a new instance of EdgeServer.
func NewEdgeServer(tunnelOperator agent.TunnelOperator, clusterService agent.ClusterService, keyService agent.EdgeKeyService) *EdgeServer {
	return &EdgeServer{
		tunnelOperator: tunnelOperator,
		clusterService: clusterService,
		keyService:     keyService,
	}
}

// Start starts a new web server by listening on the specified addr and port.
func (server *EdgeServer) Start(addr, port string) error {
	router := mux.NewRouter()
	router.HandleFunc("/init", server.handleKeySetup()).Methods(http.MethodPost)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static")))

	listenAddr := addr + ":" + port
	server.httpServer = &http.Server{Addr: listenAddr, Handler: router}

	err := server.httpServer.ListenAndServe()
	if err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (server *EdgeServer) handleKeySetup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Unable to parse form", http.StatusInternalServerError)
			return
		}

		key := r.Form.Get("key")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}

		err = server.keyService.SetKey(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if server.clusterService != nil {
			tags := server.clusterService.GetTags()
			tags[agent.MemberTagEdgeKeySet] = "set"
			err = server.clusterService.UpdateTags(tags)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			go server.propagateKeyInCluster(tags[agent.MemberTagKeyNodeName], key)
		}

		w.Write([]byte("Agent setup OK. You can close this page."))
		server.Shutdown()
	}
}

func (server *EdgeServer) propagateKeyInCluster(currentNodeName, key string) {
	httpCli := client.NewAPIClient()

	members := server.clusterService.Members()
	for _, member := range members {

		if member.NodeName == currentNodeName || member.EdgeKeySet {
			continue
		}

		memberAddr := fmt.Sprintf("%s:%s", member.IPAddress, member.Port)

		err := httpCli.SetEdgeKey(memberAddr, key)
		if err != nil {
			log.Printf("[ERROR] [edge,http] [member_address: %s] [message: Unable to propagate key to cluster member] [err: %s]", memberAddr, err)
		}
	}
}

// Shutdown is used to shutdown the server.
func (server *EdgeServer) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	server.httpServer.SetKeepAlivesEnabled(false)
	return server.httpServer.Shutdown(ctx)
}
