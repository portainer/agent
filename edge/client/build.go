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

type edgeHTTPClient struct {
	httpClient    *http.Client
	options       *agent.Options
	revokeService *revoke.Service
	certMTime     time.Time
	keyMTime      time.Time
	caMTime       time.Time
}

func BuildHTTPClient(timeout float64, options *agent.Options) *edgeHTTPClient {
	revokeService := revoke.NewService()

	c := &edgeHTTPClient{
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		options:       options,
		revokeService: revokeService,
	}

	c.httpClient.Transport = c.buildTransport()

	return c
}

func (c *edgeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c.certsNeedsRotation() {
		log.Debug().Msg("reloading certificates")
		c.httpClient.Transport = c.buildTransport()
	}

	return c.httpClient.Do(req)
}

func fileModified(filename string, mtime time.Time) bool {
	stat, err := os.Stat(filename)

	return err == nil && stat.ModTime() != mtime
}

func (c *edgeHTTPClient) certsNeedsRotation() bool {
	if c.options.SSLCert == "" || c.options.SSLKey == "" || c.options.SSLCACert == "" {
		return false
	}

	return fileModified(c.options.SSLCert, c.certMTime) ||
		fileModified(c.options.SSLKey, c.keyMTime) ||
		fileModified(c.options.SSLCACert, c.caMTime)
}

func (c *edgeHTTPClient) buildTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	transport.TLSClientConfig = crypto.CreateTLSConfiguration()
	transport.TLSClientConfig.ClientSessionCache = tls.NewLRUClientSessionCache(0)

	if c.options.EdgeInsecurePoll {
		transport.TLSClientConfig.InsecureSkipVerify = true

		return transport
	}

	if c.options.SSLCert == "" || c.options.SSLKey == "" {
		return transport
	}

	if certStat, err := os.Stat(c.options.SSLCert); err == nil {
		c.certMTime = certStat.ModTime()
	}

	if keyStat, err := os.Stat(c.options.SSLKey); err == nil {
		c.keyMTime = keyStat.ModTime()
	}

	// Create a CA certificate pool and add cert.pem to it
	if c.options.SSLCACert != "" {
		caCert, err := os.ReadFile(c.options.SSLCACert)
		if err != nil {
			log.Fatal().Err(err).Msg("")
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		transport.TLSClientConfig.RootCAs = caCertPool

		if caStat, err := os.Stat(c.options.SSLCACert); err == nil {
			c.caMTime = caStat.ModTime()
		}
	}

	transport.TLSClientConfig.GetClientCertificate = func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(c.options.SSLCert, c.options.SSLKey)

		return &cert, err
	}

	transport.TLSClientConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		for _, chain := range verifiedChains {
			for _, cert := range chain {
				revoked, err := c.revokeService.VerifyCertificate(cert)
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

	return transport
}
