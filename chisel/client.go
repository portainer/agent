package chisel

import (
	"context"

	"github.com/portainer/agent"

	chclient "github.com/jpillora/chisel/client"
)

// Client is used to create a reverse proxy tunnel connected to a Portainer instance.
type Client struct {
	chiselClient *chclient.Client
	tunnelOpen   bool
}

// NewClient creates a new reverse tunnel client
func NewClient() *Client {
	return &Client{
		tunnelOpen: false,
	}
}

// CreateTunnel will create a reverse tunnel
func (client *Client) CreateTunnel(tunnelConfig agent.TunnelConfig) error {
	// TODO: investigate the addition of a timeout via a context timeout instead
	// of using context.Background()

	config := &chclient.Config{
		Server:      tunnelConfig.ServerAddr,
		Remotes:     []string{"R:" + tunnelConfig.RemotePort + ":" + "localhost:9001"},
		Fingerprint: tunnelConfig.ServerFingerpint,
		Auth:        tunnelConfig.Credentials,
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

	client.tunnelOpen = true

	return nil
}

// CloseTunnel will close the associated chisel client
func (client *Client) CloseTunnel() error {
	client.tunnelOpen = false
	return client.chiselClient.Close()
}

// IsTunnelOpen returns true if the tunnel is created
func (client *Client) IsTunnelOpen() bool {
	return client.tunnelOpen
}
