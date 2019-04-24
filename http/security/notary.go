package security

import (
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
)

type NotaryService struct {
	signatureService agent.DigitalSignatureService
	signatureEnabled bool
}

func NewNotaryService(signatureService agent.DigitalSignatureService) *NotaryService {
	return &NotaryService{
		signatureService: signatureService,
		signatureEnabled: false,
	}
}

func (service *NotaryService) DigitalSignatureVerification(next http.Handler) http.Handler {
	return httperror.LoggerHandler(func(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {

		if service.signatureEnabled {
			publicKeyHeaderValue := r.Header.Get(agent.HTTPPublicKeyHeaderName)
			signatureHeaderValue := r.Header.Get(agent.HTTPSignatureHeaderName)

			if publicKeyHeaderValue == "" || signatureHeaderValue == "" {
				return &httperror.HandlerError{http.StatusForbidden, "Missing request signature headers", agent.ErrUnauthorized}
			}

			valid, err := service.signatureService.VerifySignature(signatureHeaderValue, publicKeyHeaderValue)
			if err != nil {
				return &httperror.HandlerError{http.StatusForbidden, "Invalid request signature", err}
			} else if !valid {
				return &httperror.HandlerError{http.StatusForbidden, "Invalid request signature", agent.ErrUnauthorized}
			}
		}

		next.ServeHTTP(rw, r)
		return nil
	})
}
