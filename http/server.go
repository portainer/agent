package http

import (
	"encoding/base64"
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

	decodedSignature, err := base64.RawStdEncoding.DecodeString(signatureHeaderValue)
	if err != nil {
		return agent.ErrUnauthorized
	}

	if !server.signatureService.ValidSignature(decodedSignature) {
		return agent.ErrUnauthorized
	}

	return nil
}

func (server *Server) digitalSignatureVerification(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		publicKeyHeaderValue := request.Header.Get(agent.HTTPPublicKeyHeaderName)
		if server.signatureService.RequiresPublicKey() && publicKeyHeaderValue == "" {
			httperror.WriteErrorResponse(rw, agent.ErrPublicKeyUnavailable, http.StatusForbidden, nil)
			return
		}

		if server.signatureService.RequiresPublicKey() && publicKeyHeaderValue != "" {
			err := server.signatureService.ParsePublicKey(publicKeyHeaderValue)
			if err != nil {
				httperror.WriteErrorResponse(rw, err, http.StatusInternalServerError, nil)
				return
			}
		}

		signatureHeaderValue := request.Header.Get(agent.HTTPSignatureHeaderName)
		err := server.verifySignature(signatureHeaderValue)
		if err != nil {
			httperror.WriteErrorResponse(rw, err, http.StatusForbidden, nil)
			return
		}

		next.ServeHTTP(rw, request)
	})
}

// Start starts a new webserver by listening on the specified listenAddr.
func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.clusterService, server.agentTags)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, server.digitalSignatureVerification(h))
}
