package client

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/edge/revoke"
)

func BuildHTTPClient(timeout float64, options *agent.Options) *http.Client {
	return &http.Client{
		Transport: buildTransport(options),
		Timeout:   time.Duration(timeout) * time.Second,
	}
}

func buildTransport(options *agent.Options) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	transport.TLSClientConfig = &tls.Config{
		ClientSessionCache: tls.NewLRUClientSessionCache(0),
		MinVersion:         tls.VersionTLS12,
		CipherSuites:       crypto.TLS12CipherSuites,
	}

	if options.EdgeInsecurePoll {
		transport.TLSClientConfig.InsecureSkipVerify = true
		return transport
	}

	if options.SSLCert != "" && options.SSLKey != "" {
		revokeService := revoke.NewService()

		// Create a CA certificate pool and add cert.pem to it
		var caCertPool *x509.CertPool
		if options.SSLCACert != "" {
			caCert, err := os.ReadFile(options.SSLCACert)
			if err != nil {
				log.Fatal().Err(err).Msg("")
			}
			caCertPool = x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
		}

		transport.TLSClientConfig.RootCAs = caCertPool
		transport.TLSClientConfig.GetClientCertificate = func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(options.SSLCert, options.SSLKey)

			return &cert, err
		}

		transport.TLSClientConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			for _, chain := range verifiedChains {
				for _, cert := range chain {
					revoked, err := revokeService.VerifyCertificate(cert)
					if err != nil {
						return err
					}

					if revoked {
						return errors.New("certificate has been revoked")
					}
				}
			}

			return nil
		}

	}

	return transport
}
