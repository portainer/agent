package http

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/handler"
	httperror "github.com/portainer/libhttp/error"
)

// Server is the web server exposing the API of an agent.
type Server struct {
	systemService    agent.SystemService
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	agentTags        map[string]string
	agentOptions     *agent.AgentOptions
}

// NewServer returns a pointer to a Server.
func NewServer(systemService agent.SystemService, clusterService agent.ClusterService, signatureService agent.DigitalSignatureService, agentTags map[string]string, agentOptions *agent.AgentOptions) *Server {
	return &Server{
		systemService:    systemService,
		clusterService:   clusterService,
		signatureService: signatureService,
		agentTags:        agentTags,
		agentOptions:     agentOptions,
	}
}

func (server *Server) verifySignature(signatureHeaderValue string) error {
	if signatureHeaderValue == "" {
		return agent.ErrUnauthorized
	}

	if !server.signatureService.ValidSignature(signatureHeaderValue) {
		return agent.ErrUnauthorized
	}

	return nil
}

func (server *Server) digitalSignatureVerification(next http.Handler) http.Handler {
	return httperror.LoggerHandler(func(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

		rw.Header().Set(agent.HTTPResponseAgentHeaderName, agent.AgentVersion)
		rw.Header().Set(agent.HTTPResponseAgentApiVersion, agent.APIVersion)

		publicKeyHeaderValue := r.Header.Get(agent.HTTPPublicKeyHeaderName)
		if server.signatureService.RequiresPublicKey() && publicKeyHeaderValue == "" {
			return &httperror.HandlerError{http.StatusForbidden, "Missing Portainer public key", agent.ErrPublicKeyUnavailable}
		}

		if server.signatureService.RequiresPublicKey() && publicKeyHeaderValue != "" {
			err := server.signatureService.ParsePublicKey(publicKeyHeaderValue)
			if err != nil {
				return &httperror.HandlerError{http.StatusInternalServerError, "Unable to parse Portainer public key", err}
			}
		}

		signatureHeaderValue := r.Header.Get(agent.HTTPSignatureHeaderName)
		err := server.verifySignature(signatureHeaderValue)
		if err != nil {
			return &httperror.HandlerError{http.StatusForbidden, "Unable to verify Portainer signature", err}
		}

		next.ServeHTTP(rw, r)
		return nil
	})
}

// Start starts a new webserver by listening on the specified listenAddr.
func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.systemService, server.clusterService, server.agentTags, server.agentOptions)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, server.digitalSignatureVerification(h))
}
