package http

import (
	"log"
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

func (server *Server) checkDigitalSignature(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		publicKeyHeader := request.Header.Get("X-PortainerAgent-PublicKey")
		if server.signatureService.RequiresPublicKey() && publicKeyHeader == "" {
			httperror.WriteErrorResponse(rw, agent.ErrPublicKeyUnavailable, http.StatusForbidden, nil)
			return
		}

		if server.signatureService.RequiresPublicKey() && publicKeyHeader != "" {
			err := server.signatureService.ParsePublicKey(publicKeyHeader)
			if err != nil {
				httperror.WriteErrorResponse(rw, err, http.StatusInternalServerError, nil)
				return
			}
			log.Println("Broadcasting !")
			err = server.clusterService.Broadcast("pubkey", []byte(publicKeyHeader))
			if err != nil {
				httperror.WriteErrorResponse(rw, err, http.StatusInternalServerError, nil)
				return
			}
		}

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

// Start starts a new webserver by listening on the specified listenAddr.
func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.clusterService, server.agentTags)
	return http.ListenAndServeTLS(listenAddr, agent.TLSCertPath, agent.TLSKeyPath, server.checkDigitalSignature(h))
}
