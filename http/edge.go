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
	// thing can be init once and then used in handler (favor reads)
	//thing := prepareThing()
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: should not be useful anymore with gorilla.mux
		//if r.URL.Path != "/init" {
		//	http.NotFound(w, r)
		//	return
		//}
		//
		//if r.Method != http.MethodPost {
		//	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		//	return
		//}

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

		// TODO: key association still needed?
		//server.Key = key

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

// TODO: rename to EdgeServer
// +REFACTOR
// +OVERHAUL
// Mainly inspired by https://stackoverflow.com/questions/39320025/how-to-stop-http-listenandserve
//type FileServer struct {
//	addr   string
//	port   string
//	server *http.Server
//	Key    string
//}
//
//type KeyCallback func(key, server string) error
//
//type serverHandler struct {
//	fileHandler http.Handler
//	initHandler http.Handler
//}
//
//func (server *FileServer) Start() error {
//	err := server.server.ListenAndServe()
//	if err != http.ErrServerClosed {
//		return err
//	}
//
//	return nil
//	//return server.server.ListenAndServe()
//	//return http.ListenAndServe(server.addr+":"+server.port, handler)
//}
//
//func (server *FileServer) Shutdown() error {
//	// TODO: investigate other context?
//	// need timeout?
//	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
//	// TODO: ignore error handling?
//	return server.server.Shutdown(ctx)
//	//if err := server.server.Shutdown(ctx); err != nil {
//	//	log.Printf("Unable to gracefully shutdown server: %s\n", err)
//	//	http.Error(w, err.Error(), http.StatusInternalServerError)
//	//	return
//	//}
//}
//
//func (server *FileServer) newServerHandler(callback KeyCallback, tunnelServer string) *serverHandler {
//	return &serverHandler{
//		fileHandler: http.FileServer(http.Dir("./static")),
//		initHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			if r.URL.Path != "/init" {
//				http.NotFound(w, r)
//				return
//			}
//
//			if r.Method != http.MethodPost {
//				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
//				return
//			}
//
//			err := r.ParseForm()
//			if err != nil {
//				http.Error(w, "Unable to parse form", http.StatusInternalServerError)
//				return
//			}
//
//			key := r.Form.Get("key")
//			if key == "" {
//				http.Error(w, "Missing key parameter", http.StatusBadRequest)
//				return
//			}
//
//			server.Key = key
//
//			err = callback(key, tunnelServer)
//			if err != nil {
//				http.Error(w, err.Error(), http.StatusInternalServerError)
//				return
//			}
//
//			// TODO: investigate other context?
//			// need timeout?
//			ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
//			// TODO: ignore error handling?
//			server.server.Shutdown(ctx)
//			//if err := server.server.Shutdown(ctx); err != nil {
//			//	log.Printf("Unable to gracefully shutdown server: %s\n", err)
//			//	http.Error(w, err.Error(), http.StatusInternalServerError)
//			//	return
//			//}
//		}),
//	}
//}
//
//func (h *serverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
//	switch {
//	case strings.HasPrefix(r.URL.Path, "/init"):
//		h.initHandler.ServeHTTP(w, r)
//	default:
//		h.fileHandler.ServeHTTP(w, r)
//	}
//}
//
//func NewFileServer(addr, port string, callback KeyCallback, tunnelServer string) *FileServer {
//
//	fserver := &FileServer{}
//
//	handler := fserver.newServerHandler(callback, tunnelServer)
//
//	fserver.server = &http.Server{Addr: addr + ":" + port, Handler: handler}
//
//	//return &FileServer{
//	//	addr:   addr,
//	//	port:   port,
//	//	server: ,
//	//}
//	return fserver
//}
