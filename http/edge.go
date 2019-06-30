package http

import (
	"context"
	"net/http"
	"time"

	"github.com/portainer/agent/filesystem"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
)

// TODO: document functions

type EdgeServer struct {
	httpServer     *http.Server
	tunnelOperator agent.TunnelOperator
}

func NewEdgeServer(tunnelOperator agent.TunnelOperator) *EdgeServer {
	return &EdgeServer{
		tunnelOperator: tunnelOperator,
	}
}

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

		err = server.tunnelOperator.SetKey(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = filesystem.WriteFile("/etc/portainer", "agent_edge_key", []byte(key), 0444)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// TODO: tunnel operator is the service used to short-poll Portainer instance
		// at the moment, it is only started on the node where the key was specified (edge cluster initiator)
		// if we want schedules to be executed/scheduled on each node inside the cluster, then all the nodes must be
		// able to poll the Portainer instance to retrieve schedules.
		// Note: only one of the nodes should create the reverse tunnel.
		// Start() is usually trigger after the SetKey function which makes me think that there should be a cluster
		// notification in between.
		// @@SWARM_SUPPORT
		go server.tunnelOperator.Start()

		w.Write([]byte("Agent setup OK. You can close this page."))
		server.Shutdown()
	}
}

func (server *EdgeServer) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	server.httpServer.SetKeepAlivesEnabled(false)
	return server.httpServer.Shutdown(ctx)
}
