package client

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/crypto"
	"github.com/portainer/agent/edge/revoke"
)

type edgeHTTPClient struct {
	httpClient       *http.Client
	options          *agent.Options
	revokeService    *revoke.Service
	certMTime        time.Time
	keyMTime         time.Time
	caMTime          time.Time
	mu               sync.RWMutex
	localAddr        *net.TCPAddr
	verifiedChains   [][]*x509.Certificate // verifiedChains is used to store the verified certificate chains from the mTLS handshake
	verifiedChainsMu sync.RWMutex
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

	c.mu.Lock()
	c.httpClient.Transport = c.buildTransport()
	c.mu.Unlock()

	return c
}

func (c *edgeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c.certsNeedsRotation() {
		log.Debug().Msg("reloading certificates")

		c.mu.Lock()
		c.httpClient.Transport = c.buildTransport()
		c.mu.Unlock()
	}

	// If mTLS is enforced and there is an established connection, check validity of server certificates
	if !c.options.EdgeInsecurePoll {
		if err := c.verifyPeerCertificate(nil, c.getVerifiedChains()); err != nil {
			c.mu.Lock()
			c.httpClient.Transport = c.buildTransport()
			c.mu.Unlock()
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.httpClient.Do(req)
}

func (c *edgeHTTPClient) SetLocalAddr(localAddr *net.TCPAddr) {
	c.mu.Lock()
	c.localAddr = localAddr
	c.httpClient.Transport = c.buildTransport()
	c.mu.Unlock()
}

func fileModified(filename string, mtime time.Time) bool {
	stat, err := os.Stat(filename)

	return err == nil && stat.ModTime() != mtime
}

func (c *edgeHTTPClient) certsNeedsRotation() bool {
	if c.options.EdgeInsecurePoll || c.options.SSLCert == "" || c.options.SSLKey == "" || c.options.SSLCACert == "" {
		return false
	}

	return fileModified(c.options.SSLCert, c.certMTime) ||
		fileModified(c.options.SSLKey, c.keyMTime) ||
		fileModified(c.options.SSLCACert, c.caMTime)
}

func (c *edgeHTTPClient) buildTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if c.localAddr != nil {
		transport.DialContext = (&net.Dialer{
			LocalAddr: c.localAddr,
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

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

	transport.TLSClientConfig.VerifyPeerCertificate = c.verifyPeerCertificate

	transport.TLSClientConfig.VerifyConnection = func(state tls.ConnectionState) error {
		// Note, this callback is called during the TLS handshake, but after the VerifyPeerCertificate callback.
		// The state argument contains the verified chains, which are used to check if certificates have been revoked, or expired.
		c.setVerifiedChains(state.VerifiedChains)

		return nil
	}

	return transport
}

// verifyPeerCertificate is a callback that adheres to the tls.Config.VerifyPeerCertificate signature. It is used to
// verify the peer certificate chain and check if the certificate has been revoked.
func (c *edgeHTTPClient) verifyPeerCertificate(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
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

func (c *edgeHTTPClient) setVerifiedChains(chains [][]*x509.Certificate) {
	c.verifiedChainsMu.Lock()
	defer c.verifiedChainsMu.Unlock()

	c.verifiedChains = chains
}

func (c *edgeHTTPClient) getVerifiedChains() [][]*x509.Certificate {
	c.verifiedChainsMu.RLock()
	defer c.verifiedChainsMu.RUnlock()

	verifiedChains := make([][]*x509.Certificate, len(c.verifiedChains))
	for i, chain := range c.verifiedChains {
		verifiedChains[i] = make([]*x509.Certificate, len(chain))
		copy(verifiedChains[i], chain)
	}

	return verifiedChains
}
