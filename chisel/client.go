package chisel

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"

	chclient "github.com/jpillora/chisel/client"
)

const tunnelClientTimeout = 10 * time.Second

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
	remote := fmt.Sprintf("R:%s:%s", tunnelConfig.RemotePort, tunnelConfig.LocalAddr)

	log.Printf("[DEBUG] [edge,chisel] [remote_port: %s] [local_addr: %s] [server: %s] [server_fingerprint: %s] [message: Creating reverse tunnel client]", tunnelConfig.RemotePort, tunnelConfig.LocalAddr, tunnelConfig.ServerAddr, tunnelConfig.ServerFingerpint)

	config := &chclient.Config{
		Server:      tunnelConfig.ServerAddr,
		Remotes:     []string{remote},
		Fingerprint: tunnelConfig.ServerFingerpint,
		Auth:        tunnelConfig.Credentials,
	}

	chiselClient, err := chclient.NewClient(config)
	if err != nil {
		return err
	}

	client.chiselClient = chiselClient

	ctx, cancel := context.WithTimeout(context.Background(), tunnelClientTimeout)
	defer cancel()

	err = chiselClient.Start(ctx)
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
