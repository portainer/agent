package http

import (
	"net/http"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/handler"
)

func NewServer(clusterService agent.ClusterService, signatureService agent.DigitalSignatureService, agentTags map[string]string) *Server {
	return &Server{
		clusterService:   clusterService,
		signatureService: signatureService,
		agentTags:        agentTags,
	}
}

type Server struct {
	clusterService   agent.ClusterService
	signatureService agent.DigitalSignatureService
	agentTags        map[string]string
}

func (server *Server) checkDigitalSignature(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		signatureHeader := request.Header.Get(agent.HTTPSignatureHeaderName)
		if signatureHeader == "" {
			httperror.WriteErrorResponse(rw, agent.ErrUnauthorized, http.StatusForbidden, nil)
			return
		}

		if !server.signatureService.ValidSignature(signatureHeader) {
			httperror.WriteErrorResponse(rw, agent.ErrUnauthorized, http.StatusForbidden, nil)
			return
		}
		next.ServeHTTP(rw, request)
	})
}

func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.clusterService, server.agentTags)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, server.checkDigitalSignature(h))
}
