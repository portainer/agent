package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
)

func NewNomadProxy(nomadConfig agent.NomadConfig) http.Handler {
	remoteURL, _ := url.Parse(nomadConfig.NomadAddr)

	proxy := httputil.NewSingleHostReverseProxy(remoteURL)

	if nomadConfig.NomadTLSEnabled {
		tlsClientConfig := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			CipherSuites: crypto.TLS12CipherSuites,
			MaxVersion:   tls.VersionTLS13,
		}

		// Create a CA certificate pool and add cert.pem to it
		if nomadConfig.NomadCACert != "" {
			var caCertPool *x509.CertPool
			caCert, err := ioutil.ReadFile(nomadConfig.NomadCACert)
			if err != nil {
				log.Fatalf("[ERROR] [proxy,nomad] [message: failed to read Nomad CA Cert]")
			}
			caCertPool = x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			tlsClientConfig.RootCAs = caCertPool
		}

		if nomadConfig.NomadClientCert != "" && nomadConfig.NomadClientKey != "" {
			tlsClientConfig.GetClientCertificate = func(chi *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				cert, err := tls.LoadX509KeyPair(nomadConfig.NomadClientCert, nomadConfig.NomadClientKey)
				if err != nil {
					return nil, err
				}

				return &cert, nil
			}
		}

		// Create an HTTPS client and supply the created CA pool and certificate
		proxy.Transport = &http.Transport{
			TLSClientConfig: tlsClientConfig,
		}
	} else {
		proxy.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	return proxy
}
