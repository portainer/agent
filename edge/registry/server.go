package registry

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/portainer/agent"
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
	stackManager := handler.EdgeManager.GetStackManager()
	if stackManager == nil {
		return
	}

	serverUrl, _ := request.RetrieveQueryParameter(r, "serverurl", false)

	credentials := stackManager.GetEdgeRegistryCredentials()
	if len(credentials) > 0 {
		log.Printf("ServerURL=%s", serverUrl)

		var key string
		if strings.HasPrefix(serverUrl, "http") {
			u, err := url.Parse(serverUrl)
			if err != nil {
				response.Empty(rw)
				return
			}

			if strings.HasSuffix(u.Hostname(), "docker.io") {
				key = "docker.io"
			} else {
				key = u.Hostname()
			}
		} else {
			key = serverUrl
		}

		log.Printf("[INFO] [main] [message: Looking up credentials for %s]\n", key)

		log.Printf("[DEBUG] Credentials: %+v", credentials)
		for _, c := range credentials {
			if key == c.ServerURL {
				response.JSON(rw, c)
				return
			}
		}
	}

	response.Empty(rw)
}

func LookupCredentials(credentials []agent.RegistryCredentials, serverUrl string) (*agent.RegistryCredentials, error) {
	u, err := url.Parse(serverUrl)
	if err != nil {
		return nil, err
	}

	var key string
	if strings.HasSuffix(u.Hostname(), ".docker.io") {
		key = "docker.io"
	} else {
		key = u.Hostname()
	}

	for _, c := range credentials {
		if key == c.ServerURL {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("No credentials found for %s", serverUrl)
}

func StartRegistryServer(edgeManager *edge.Manager) (err error) {
	log.Println("[INFO] [main] [message: Starting registry server]")
	h := NewEdgeRegistryHandler(edgeManager)

	server := &http.Server{
		Addr:         "127.0.0.1:9005",
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      h,
	}

	// run in a goroutine so it doesn't block
	go func() {
		err = server.ListenAndServe()
	}()

	return err
}
