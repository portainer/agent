package registry

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge"
	"github.com/portainer/agent/edge/aws"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	*mux.Router
	EdgeManager *edge.Manager
	awsConfig   *agent.AWSConfig
}

func NewEdgeRegistryHandler(edgeManager *edge.Manager, awsConfig *agent.AWSConfig) *Handler {
	h := &Handler{
		Router:      mux.NewRouter(),
		EdgeManager: edgeManager,
		awsConfig:   awsConfig,
	}

	h.Handle("/lookup", httperror.LoggerHandler(h.LookupHandler)).Methods(http.MethodGet)
	return h
}

func (handler *Handler) LookupHandler(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	stackManager := handler.EdgeManager.GetStackManager()
	if stackManager == nil {
		return httperror.InternalServerError("Unable to retrieve stack manager", errors.New("stack manager is not available"))
	}

	serverUrl, _ := request.RetrieveQueryParameter(r, "serverurl", false)

	log.Info().Str("server_url", r.URL.Query().Get("serverurl")).Msg("registry lookup handler looking up credentials")

	if serverUrl == "" {
		return response.Empty(rw)
	}

	if handler.awsConfig != nil {
		ecrCred, err := aws.DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverUrl, handler.awsConfig)
		if err == nil && ecrCred != nil {
			log.Info().Str("registry_server_url", serverUrl).Msg("lookup handler successfully fetched ECR credentials for private ECR repository")
			return response.JSON(rw, ecrCred)
		} else if errors.Is(err, aws.ErrNotPrivateECRRepo) {
			log.Info().Str("registry_server_url", serverUrl).Msg("lookup handler repository url is not a private ECR repository, continuing")
		} else {
			log.Error().Err(err).Str("registry_server_url", serverUrl).Msg("lookup handler failed to fetch ECR credentials for private ECR repository")
			return httperror.InternalServerError("Unable to retrieve temporary ECR credentials", err)
		}
	}

	credentials := stackManager.GetEdgeRegistryCredentials()

	if len(credentials) == 0 {
		return response.Empty(rw)
	}

	key := serverUrl

	if strings.HasPrefix(serverUrl, "http") {
		u, err := url.Parse(serverUrl)
		if err != nil {
			return httperror.BadRequest("Invalid server URL", err)
		}

		if strings.HasSuffix(u.Hostname(), "docker.io") {
			key = "docker.io"
		} else {
			key = u.Hostname()
		}
	}

	for _, c := range credentials {
		if key == c.ServerURL {
			log.Info().Str("registry_server_url", c.ServerURL).Msg("lookup handler found credentials")
			return response.JSON(rw, c)
		}
	}

	log.Info().Str("registry_server_url", key).Msg("lookup handler no credentials found")
	return response.Empty(rw)
}

func StartRegistryServer(edgeManager *edge.Manager, awsConfig *agent.AWSConfig) (err error) {
	log.Info().Msg("starting registry credential server")

	h := NewEdgeRegistryHandler(edgeManager, awsConfig)

	server := &http.Server{
		Addr:         "127.0.0.1:9005",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
		Handler:      h,
	}

	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9005})
	if err != nil {
		return err
	}

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Error in the registry credential server")
		}
	}()

	return nil
}
