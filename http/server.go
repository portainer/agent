package http

import (
	"crypto/ecdsa"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/handler"
)

func NewServer(clusterService agent.ClusterService, agentTags map[string]string, authorizedKey interface{}) *Server {
	return &Server{
		clusterService: clusterService,
		agentTags:      agentTags,
		authorizedKey:  authorizedKey,
	}
}

type Server struct {
	clusterService agent.ClusterService
	// TODO: should probably embbed a crypto service to allow changing signing/verif methods (ecdsa, rsa, etc...)
	agentTags     map[string]string
	authorizedKey interface{}
	// TODO: authorizedKey should be parsed at startup. Is there a way to validate the key?
	// authKey        *ecdsa.PublicKey
}

func isValidSignature(signature string, authorizedKey interface{}) bool {
	sign, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	r := new(big.Int).SetBytes(sign[:len(sign)/2])
	s := new(big.Int).SetBytes(sign[len(sign)/2:])
	hash := fmt.Sprintf("%x", md5.Sum([]byte(agent.PortainerAgentSignatureMessage)))
	publicKey := authorizedKey.(*ecdsa.PublicKey)

	return ecdsa.Verify(publicKey, []byte(hash), r, s)
}

func checkSignature(next http.Handler, authorizedKey interface{}) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		signatureHeader := request.Header.Get(agent.HTTPSignatureHeaderName)
		if signatureHeader == "" {
			httperror.WriteErrorResponse(rw, agent.ErrUnauthorized, http.StatusForbidden, nil)
			return
		}

		if !isValidSignature(signatureHeader, authorizedKey) {
			httperror.WriteErrorResponse(rw, agent.ErrUnauthorized, http.StatusForbidden, nil)
			return
		}
		next.ServeHTTP(rw, request)
	})
}

func (server *Server) Start(listenAddr string) error {
	h := handler.NewHandler(server.clusterService, server.agentTags)
	return http.ListenAndServeTLS(listenAddr, "cert.pem", "key.pem", checkSignature(h, server.authorizedKey))
}
