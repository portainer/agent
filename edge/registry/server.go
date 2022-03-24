package registry

import (
	"errors"
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

	httperror "github.com/portainer/libhttp/error"
)

type Handler struct {
	*mux.Router
	EdgeManager *edge.Manager
}

func NewEdgeRegistryHandler(edgeManager *edge.Manager) *Handler {
	h := &Handler{
		Router:      mux.NewRouter(),
		EdgeManager: edgeManager,
	}

	h.Handle("/lookup", httperror.LoggerHandler(h.LookupHandler)).Methods(http.MethodGet)
	return h
}

func (handler *Handler) LookupHandler(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stackManager := handler.EdgeManager.GetStackManager()
	if stackManager == nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to retrieve stack manager", errors.New("Stack manager is not available")}
	}

	serverUrl, _ := request.RetrieveQueryParameter(r, "serverurl", false)

	credentials := stackManager.GetEdgeRegistryCredentials()
	if len(credentials) > 0 {
		var key string
		if strings.HasPrefix(serverUrl, "http") {
			u, err := url.Parse(serverUrl)
			if err != nil {
				response.Empty(rw)
				return &httperror.HandlerError{http.StatusBadRequest, "Invalid server URL", err}
			}

			if strings.HasSuffix(u.Hostname(), "docker.io") {
				key = "docker.io"
			} else {
				key = u.Hostname()
			}
		} else {
			key = serverUrl
		}

		log.Printf("[INFO] [message: Looking up credentials for using serverUrl '%s' and key '%s']", serverUrl, key)

		for _, c := range credentials {
			if key == c.ServerURL {
				response.JSON(rw, c)
				return nil
			}
		}
	}

	return response.Empty(rw)
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
