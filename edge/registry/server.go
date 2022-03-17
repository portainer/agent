package registry

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/edge"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type EdgeRegistryHandler struct {
	*mux.Router
	EdgeManager *edge.Manager
}

func NewEdgeRegistryHandler(edgeManager *edge.Manager) *EdgeRegistryHandler {
	h := &EdgeRegistryHandler{
		Router:      mux.NewRouter(),
		EdgeManager: edgeManager,
	}

	h.HandleFunc("/lookup", h.LookupHandler).Methods(http.MethodGet)
	http.Handle("/", h)

	return h
}

func (handler *EdgeRegistryHandler) LookupHandler(rw http.ResponseWriter, r *http.Request) {
	sm := handler.EdgeManager.GetStackManager()

	serverurl, _ := request.RetrieveQueryParameter(r, "serverurl", false)

	if sm != nil {
		credentials := sm.GetEdgeRegistryCredentials()
		if len(credentials) > 0 {
			u, err := url.Parse(serverurl)
			if err != nil {
				return
			}

			var key string
			if u.Hostname() == "index.docker.io" {
				key = "docker.io"
			} else {
				key = u.Hostname()
			}

			for _, c := range credentials {
				if key == c.ServerURL {
					response.JSON(rw, c)
				}
			}
		}
	}
}

func StartRegistryServer(edgeManager *edge.Manager) (err error) {
	log.Println("[INFO] [main] [message: Starting registry server]")
	h := NewEdgeRegistryHandler(edgeManager)

	srv := &http.Server{
		Addr:         "127.0.0.1:9005",
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      h,
	}

	// run in a goroutine so it doesn't block
	go func() {
		err = srv.ListenAndServe()
	}()

	return err
}
