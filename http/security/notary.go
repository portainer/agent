package security

import (
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
)

type NotaryService struct {
	signatureService agent.DigitalSignatureService
}

func NewNotaryService(signatureService agent.DigitalSignatureService) *NotaryService {
	return &NotaryService{
		signatureService: signatureService,
	}
}

func (service *NotaryService) DigitalSignatureVerification(next http.Handler) http.Handler {
	return httperror.LoggerHandler(func(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

		publicKeyHeaderValue := r.Header.Get(agent.HTTPPublicKeyHeaderName)
		if service.signatureService.RequiresPublicKey() && publicKeyHeaderValue == "" {
			return &httperror.HandlerError{http.StatusForbidden, "Missing Portainer public key", agent.ErrPublicKeyUnavailable}
		}

		if service.signatureService.RequiresPublicKey() && publicKeyHeaderValue != "" {
			err := service.signatureService.ParsePublicKey(publicKeyHeaderValue)
			if err != nil {
				return &httperror.HandlerError{http.StatusInternalServerError, "Unable to parse Portainer public key", err}
			}
		}

		signatureHeaderValue := r.Header.Get(agent.HTTPSignatureHeaderName)
		err := service.verifySignature(signatureHeaderValue)
		if err != nil {
			return &httperror.HandlerError{http.StatusForbidden, "Unable to verify Portainer signature", err}
		}

		next.ServeHTTP(rw, r)
		return nil
	})
}

func (service *NotaryService) verifySignature(signatureHeaderValue string) error {
	if signatureHeaderValue == "" {
		return agent.ErrUnauthorized
	}

	if !service.signatureService.ValidSignature(signatureHeaderValue) {
		return agent.ErrUnauthorized
	}

	return nil
}
