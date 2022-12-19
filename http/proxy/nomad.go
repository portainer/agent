package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"

	"github.com/rs/zerolog/log"
)

func NewNomadProxy(nomadConfig agent.NomadConfig) http.Handler {
	remoteURL, _ := url.Parse(nomadConfig.NomadAddr)

	proxy := httputil.NewSingleHostReverseProxy(remoteURL)

	tlsConfig := crypto.CreateTLSConfiguration()
	proxy.Transport = &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	if !nomadConfig.NomadTLSEnabled {
		tlsConfig.InsecureSkipVerify = true

		return proxy
	}

	// Create a CA certificate pool and add cert.pem to it
	if nomadConfig.NomadCACert != "" {
		caCert, err := os.ReadFile(nomadConfig.NomadCACert)
		if err != nil {
			log.Fatal().Msg("failed to read Nomad CA Cert")
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		tlsConfig.RootCAs = caCertPool
	}

	if nomadConfig.NomadClientCert != "" && nomadConfig.NomadClientKey != "" {
		tlsConfig.GetClientCertificate = func(chi *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(nomadConfig.NomadClientCert, nomadConfig.NomadClientKey)
			if err != nil {
				return nil, err
			}

			return &cert, nil
		}
	}

	return proxy
}
