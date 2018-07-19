package http

import (
	"net/http"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/handler"
)

// Server is the web server exposing the API of an agent.
type Server struct {
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	agentTags        map[string]string
}

// NewServer returns a pointer to a Server.
func NewServer(clusterService agent.ClusterService, signatureService agent.DigitalSignatureService, agentTags map[string]string) *Server {
	return &Server{
		clusterService:   clusterService,
		signatureService: signatureService,
		agentTags:        agentTags,
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
	h := handler.NewHandler(server.clusterService, server.agentTags)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, server.digitalSignatureVerification(h))
}
