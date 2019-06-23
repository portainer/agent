package http

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
)

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
