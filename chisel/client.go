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

// TODO: doc
func (client *Client) CreateTunnel(tunnelConfig agent.TunnelConfig) error {

	// TODO: Should be relocated inside another function, otherwise we re-create client
	// each time we need to open a tunnel
	config := &chclient.Config{
		Server:      tunnelConfig.ServerAddr,
		Remotes:     []string{"R:" + tunnelConfig.RemotePort + ":" + "localhost:9001"},
		Fingerprint: tunnelConfig.ServerFingerpint,
		Auth:        tunnelConfig.Credentials,
	}

	// TODO: timeout? should stop and error if cannot connect after timeout?
	chiselClient, err := chclient.NewClient(config)
	if err != nil {
		return err
	}
	client.chiselClient = chiselClient

	client.tunnelOpen = true
	return chiselClient.Start(context.Background())
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
