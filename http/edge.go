package http

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/portainer/agent/internal/edge"

	"github.com/gorilla/mux"
)

// EdgeServer expose an UI to associate an Edge key with the agent.
type EdgeServer struct {
	httpServer  *http.Server
	edgeManager *edge.Manager
}

// NewEdgeServer returns a pointer to a new instance of EdgeServer.
func NewEdgeServer(edgeManager *edge.Manager) *EdgeServer {
	return &EdgeServer{
		edgeManager: edgeManager,
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

		err = server.edgeManager.SetKey(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = server.edgeManager.Start()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		go server.propagateKeyInCluster()

		w.Write([]byte("Agent setup OK. You can close this page."))
		server.Shutdown()
	}
}

func (server *EdgeServer) propagateKeyInCluster() {
	err := server.edgeManager.PropagateKeyInCluster()
	if err != nil {
		log.Printf("[ERROR] [edge,http] [message: Unable to propagate key to cluster] [err: %s]", err)
	}
}

// Shutdown is used to shutdown the server.
func (server *EdgeServer) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	server.httpServer.SetKeepAlivesEnabled(false)
	return server.httpServer.Shutdown(ctx)
}
