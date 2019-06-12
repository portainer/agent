package http

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
)

type EdgeServer struct {
	client     agent.ReverseTunnelClient
	httpServer *http.Server
}

func NewEdgeServer(client agent.ReverseTunnelClient) *EdgeServer {
	return &EdgeServer{
		client: client,
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

		err = server.client.CreateTunnel(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// TODO: error handling?
		server.Shutdown()
	}
}

// TODO: investigate whether this is the best way to shutdown a web server
// Use another context? Is timeout required?
func (server *EdgeServer) Shutdown() error {
	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	return server.httpServer.Shutdown(ctx)
}
