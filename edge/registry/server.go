package registry

import (
	"errors"
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
		return httperror.InternalServerError("Unable to retrieve stack manager", errors.New("Stack manager is not available"))
	}

	serverUrl, _ := request.RetrieveQueryParameter(r, "serverurl", false)

	log.Info().Str("server_url", serverUrl).Msg("looking up credentials")

	if serverUrl == "" {
		return response.Empty(rw)
	}

	// We could technically filter out non ECR registry URLs here and not apply this logic to all the registries
	// The cost of going through this logic for all server/registries is to authenticate against IAM RA for each registry
	// We could filter non ECR registries based on a URL pattern: https://docs.aws.amazon.com/AmazonECR/latest/userguide/Registries.html
	// BUT, to keep support for DNS aliases with ECR registries (e.g. mapping a custom domain such as myregistry.portainer.io to an ECR registry) I've decided to avoid the filter
	if handler.awsConfig != nil {
		log.Info().Msg("using local AWS config for credential lookup")

		c, err := aws.DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverUrl, handler.awsConfig)
		if err != nil && !errors.Is(err, aws.ErrNoCredentials) {
			return httperror.InternalServerError("Unable to retrieve credentials", err)
		}

		// Only write credentials if credentials are found
		// For non ECR registries, credentials will be set to nil
		// Therefore we want to fallback to the default credential lookup
		if c != nil {
			return response.JSON(rw, c)
		}
	}

	credentials := stackManager.GetEdgeRegistryCredentials()
	if len(credentials) > 0 {
		var key string
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
		} else {
			key = serverUrl
		}

		for _, c := range credentials {
			if key == c.ServerURL {
				return response.JSON(rw, c)
			}
		}
	}

	return response.Empty(rw)
}

func StartRegistryServer(edgeManager *edge.Manager, awsConfig *agent.AWSConfig) (err error) {
	log.Info().Msg("starting registry credential server")

	h := NewEdgeRegistryHandler(edgeManager, awsConfig)

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
