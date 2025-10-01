package chisel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/portainer/pkg/fips"

	chclient "github.com/jpillora/chisel/client"
	"github.com/rs/zerolog/log"
)

// Client is used to create a reverse proxy tunnel connected to a Portainer instance.
type Client struct {
	chiselClient *chclient.Client
	tunnelOpen   bool
	mu           sync.Mutex
	tlsCACert    string
	tlsCert      string
	tlsKey       string
	caCertMTime  time.Time
	certMTime    time.Time
	keyMTime     time.Time
}

// NewClient creates a new reverse tunnel client
func NewClient(tlsCACert, tlsCert, tlsKey string) *Client {
	return newClient(tlsCACert, tlsCert, tlsKey, fips.FIPSMode())
}

func newClient(tlsCACert, tlsCert, tlsKey string, fips bool) *Client {
	c := &Client{
		tunnelOpen: false,
		tlsCACert:  tlsCACert,
		tlsCert:    tlsCert,
		tlsKey:     tlsKey,
	}

	c.updateCertModifiedTimes(fips)

	return c
}

func replaceSchemaWithHTTPS(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "wss://") {
		return u
	} else if after, found := strings.CutPrefix(u, "http://"); found {
		return "https://" + after
	} else if after, found := strings.CutPrefix(u, "ws://"); found {
		return "https://" + after
	} else {
		return "https://" + u
	}
}

// CreateTunnel will create a reverse tunnel
func (client *Client) CreateTunnel(tunnelConfig agent.TunnelConfig) error {
	remote := fmt.Sprintf("R:%s:%s", tunnelConfig.RemotePort, tunnelConfig.LocalAddr)

	log.Debug().
		Str("remote_port", tunnelConfig.RemotePort).
		Str("local_addr", tunnelConfig.LocalAddr).
		Str("server", tunnelConfig.ServerAddr).
		Str("server_fingerprint", tunnelConfig.ServerFingerprint).
		Msg("creating reverse tunnel client")

	config := &chclient.Config{
		Server:      tunnelConfig.ServerAddr,
		Remotes:     []string{remote},
		Fingerprint: tunnelConfig.ServerFingerprint,
		Auth:        tunnelConfig.Credentials,
		Proxy:       tunnelConfig.Proxy,
	}

	if fips.FIPSMode() {
		config.Server = replaceSchemaWithHTTPS(config.Server)
		config.TLS = chclient.TLSConfig{
			CA:   client.tlsCACert,
			Cert: client.tlsCert,
			Key:  client.tlsKey,
		}
	}

	chiselClient, err := chclient.NewClient(config)
	if err != nil {
		return err
	}

	client.chiselClient = chiselClient

	err = chiselClient.Start(context.Background())
	if err != nil {
		return err
	}

	client.mu.Lock()
	client.tunnelOpen = true
	client.mu.Unlock()

	return nil
}

// CloseTunnel will close the associated chisel client
func (client *Client) CloseTunnel() error {
	client.mu.Lock()
	client.tunnelOpen = false
	client.updateCertModifiedTimes(fips.FIPSMode())
	client.mu.Unlock()

	return client.chiselClient.Close()
}

// IsTunnelOpen returns true if the tunnel is created
func (client *Client) IsTunnelOpen() bool {
	client.mu.Lock()
	defer client.mu.Unlock()

	return client.tunnelOpen
}

func (client *Client) CertsNeedRotation() bool {
	return client.certsNeedRotation(fips.FIPSMode())
}

func (client *Client) certsNeedRotation(fips bool) bool {
	if !fips {
		return false
	}

	return fileModified(client.tlsCert, client.certMTime) ||
		fileModified(client.tlsKey, client.keyMTime) ||
		fileModified(client.tlsCACert, client.caCertMTime)
}

func (client *Client) updateCertModifiedTimes(fips bool) {
	if !fips {
		return
	}

	if caCertStat, err := os.Stat(client.tlsCACert); err == nil {
		client.caCertMTime = caCertStat.ModTime()
	}

	if certStat, err := os.Stat(client.tlsCert); err == nil {
		client.certMTime = certStat.ModTime()
	}

	if keyStat, err := os.Stat(client.tlsKey); err == nil {
		client.keyMTime = keyStat.ModTime()
	}
}

func fileModified(filename string, mtime time.Time) bool {
	stat, err := os.Stat(filename)

	return err == nil && stat.ModTime() != mtime
}
