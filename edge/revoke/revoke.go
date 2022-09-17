// Package revoke provides functionality for checking the validity of
// a cert. Specifically, the temporal validity of the certificate is
// checked first, then any CRL and OCSP url in the cert is checked.
// ported from https://github.com/cloudflare/cfssl/blob/master/revoke/revoke.go
package revoke

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	neturl "net/url"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	defaultTimeoutInSeconds = 3
)

type Service struct {
	httpClient *http.Client
	// hardFail determines whether the failure to check the revocation
	// status of a certificate (i.e. due to network failure) causes
	// verification to fail (a hard failure).
	hardFail bool
	// crlSet associates a PKIX certificate list with the URL the CRL is
	// fetched from.
	crlSet  map[string]*pkix.CertificateList
	crlLock sync.Mutex
}

func NewService() *Service {
	return &Service{
		httpClient: &http.Client{
			Timeout: defaultTimeoutInSeconds * time.Second,
		},
		hardFail: false,
		crlSet:   make(map[string]*pkix.CertificateList),
	}
}

// VerifyCertificate ensures that the certificate passed in hasn't
// expired and checks the CRL for the server.
func (service *Service) VerifyCertificate(cert *x509.Certificate) (revoked bool, err error) {
	// certificate expired
	if !time.Now().Before(cert.NotAfter) {
		log.Info().Time("not_after", cert.NotAfter).Msg("certificate expired")

		return true, fmt.Errorf("certificate expired %s", cert.NotAfter)
	}

	// certificate is not yet valid
	if !time.Now().After(cert.NotBefore) {
		log.Info().Time("not_before", cert.NotBefore).Msg("certificate isn't valid yet")

		return true, fmt.Errorf("certificate isn't valid until %s", cert.NotBefore)
	}

	return service.revCheck(cert)
}

// revCheck should check the certificate for any revocations.
func (service *Service) revCheck(cert *x509.Certificate) (revoked bool, err error) {
	for _, url := range cert.CRLDistributionPoints {
		if ldapURL(url) {
			log.Info().Str("url", url).Msg("skipping LDAP CRL")

			continue
		}

		revoked, err := service.certIsRevokedCRL(cert, url)
		if err != nil {
			log.Warn().Msg("error checking revocation via CRL")

			return service.hardFail, err
		}

		if revoked {
			log.Info().Msg("certificate is revoked via CRL")

			return true, err
		}
	}

	return false, nil
}

// We can't handle LDAP certificates, so this checks to see if the
// URL string points to an LDAP resource so that we can ignore it.
func ldapURL(url string) bool {
	u, err := neturl.Parse(url)
	if err != nil {
		log.Warn().Str("url", url).Err(err).Msg("error parsing URL")

		return false
	}

	return u.Scheme == "ldap"
}

// certIsRevokedCRL checks a cert against a specific CRL. Returns the same bool pair
// as revCheck, plus an error if one occurred.
func (service *Service) certIsRevokedCRL(cert *x509.Certificate, url string) (revoked bool, err error) {
	service.crlLock.Lock()
	crl, ok := service.crlSet[url]
	if ok && crl == nil {
		ok = false
		delete(service.crlSet, url)
	}
	service.crlLock.Unlock()

	var shouldFetchCRL = true
	if ok {
		if !crl.HasExpired(time.Now()) {
			shouldFetchCRL = false
		}
	}

	issuer := service.getIssuer(cert)

	if shouldFetchCRL {
		var err error
		crl, err = service.fetchCRL(url)
		if err != nil {
			log.Warn().Str("url", url).Err(err).Msg("failed fetching CRL")

			return false, err
		}

		// check CRL signature
		if issuer != nil {
			err = issuer.CheckCRLSignature(crl)
			if err != nil {
				log.Warn().Str("url", url).Err(err).Msg("failed verifying CRL")

				return false, err
			}
		}

		service.crlLock.Lock()
		service.crlSet[url] = crl
		service.crlLock.Unlock()
	}

	for _, revoked := range crl.TBSCertList.RevokedCertificates {
		if cert.SerialNumber.Cmp(revoked.SerialNumber) == 0 {
			return true, nil
		}
	}

	return false, nil
}

// fetchCRL fetches and parses a CRL.
func (service *Service) fetchCRL(url string) (*pkix.CertificateList, error) {
	resp, err := service.httpClient.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, errors.New("failed to retrieve CRL")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return x509.ParseCRL(body)
}

func (service *Service) getIssuer(cert *x509.Certificate) *x509.Certificate {
	for _, issuingCert := range cert.IssuingCertificateURL {
		issuer, err := service.fetchRemote(issuingCert)
		if err != nil {
			continue
		}

		return issuer
	}

	return nil
}

func (service *Service) fetchRemote(url string) (*x509.Certificate, error) {
	resp, err := service.httpClient.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	in, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	p, _ := pem.Decode(in)
	if p != nil {
		return parseCertificatePEM(in)
	}

	return x509.ParseCertificate(in)
}

// parseCertificatePEM parses and returns a PEM-encoded certificate,
// can handle PEM encoded PKCS #7 structures.
func parseCertificatePEM(certPEM []byte) (*x509.Certificate, error) {
	certPEM = bytes.TrimSpace(certPEM)

	cert, rest, err := parseOneCertificateFromPEM(certPEM)
	if err != nil {
		// Log the actual parsing error but throw a default parse error message.
		log.Debug().Err(err).Msg("certificate parsing error")

		return nil, errors.Wrap(err, "unable to parse certificate")
	}

	if cert == nil {
		return nil, errors.New("failed to decode certificate")
	}

	if len(rest) > 0 {
		return nil, errors.New("the PEM file should contain only one object")
	}

	if len(cert) > 1 {
		return nil, errors.New("the PKCS7 object in the PEM file should contain only one certificate")
	}

	return cert[0], nil
}

// parseOneCertificateFromPEM attempts to parse one PEM encoded certificate object,
// either a raw x509 certificate or a PKCS #7 structure possibly containing
// multiple certificates, from the top of certsPEM, which itself may
// contain multiple PEM encoded certificate objects.
func parseOneCertificateFromPEM(certsPEM []byte) (certs []*x509.Certificate, rest []byte, err error) {
	block, rest := pem.Decode(certsPEM)
	if block == nil {
		return nil, rest, nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		pkcs7data, err := parsePKCS7(block.Bytes)
		if err != nil {
			return nil, rest, err
		}

		if pkcs7data.ContentInfo != "SignedData" {
			return nil, rest, errors.New("only PKCS #7 Signed Data Content Info supported for certificate parsing")
		}

		certs = pkcs7data.Content.SignedData.Certificates
		if certs == nil {
			return nil, rest, errors.New("PKCS #7 structure contains no certificates")
		}

		return certs, rest, nil
	}

	return []*x509.Certificate{cert}, rest, nil
}
