package chisel

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"strings"

	chclient "github.com/jpillora/chisel/client"
)

// keyInformation represent the information extracted from an encoded Edge key
type keyInformation struct {
	TunnelServerAddr        string
	TunnelServerPort        string
	TunnelServerFingerprint string
	TunnelPort              string
	Credentials             string
}

// Client is used to create a reverse proxy tunnel connected to a Portainer instance.
type Client struct {
	key                 *keyInformation
	tunnelServerAddress string
}

// NewClient creates a new reverse tunnel client
// It stores the specified tunnel server address and uses it
// if the server address specified in the key equals to localhost
// TODO: this tunnel server address override is a work-around for a problem on Portainer side.
// The server address is currently retrieved from the browser host when creating an endpoint inside Portainer
// and can be equal to localhost when using a local deployment of Portainer (http://localhost:9000)
// This override can be set via the EDGE_TUNNEL_SERVER env var.
// This should be documented in the README or simply prevent the use of Edge when connected to a localhost instance.
func NewClient(tunnelServerAddr string) *Client {
	return &Client{
		tunnelServerAddress: tunnelServerAddr,
	}
}

// parseEdgeKey decodes a base64 encoded key and extract the decoded information from the following
// format: tunnel_server_addr|tunnel_server_port|tunnel_server_fingerprint|tunnel_port|credentials
// credentials are expected in the user:password format
func parseEdgeKey(key string) (*keyInformation, error) {
	decodedKey, err := base64.RawStdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}

	keyInfo := strings.Split(string(decodedKey), "|")

	if len(keyInfo) != 5 {
		return nil, errors.New("invalid key format")
	}

	keyInformation := &keyInformation{
		TunnelServerAddr:        keyInfo[0],
		TunnelServerPort:        keyInfo[1],
		TunnelServerFingerprint: keyInfo[2],
		TunnelPort:              keyInfo[3],
		Credentials:             keyInfo[4],
	}

	return keyInformation, nil
}

// IsKeySet returns true if a key has been associated with this client.
func (client *Client) IsKeySet() bool {
	if client.key == nil {
		return false
	}
	return true
}

// CreateTunnel will parse the encoded key to retrieve the information
// required to create a tunnel.
// This function also associates the key to the client.
func (client *Client) CreateTunnel(key string) error {
	keyInformation, err := parseEdgeKey(key)
	if err != nil {
		return err
	}

	if keyInformation.TunnelServerAddr == "localhost" {
		if client.tunnelServerAddress == "" {
			return errors.New("cannot use localhost as tunnel server address")
		}
		keyInformation.TunnelServerAddr = client.tunnelServerAddress
		log.Printf("[DEBUG] [edge,rtunnel] [tunnel_server_addr: %s] [message: overriding tunnel server address]", keyInformation.TunnelServerAddr)
	}

	client.key = keyInformation

	config := &chclient.Config{
		Server:      keyInformation.TunnelServerAddr + ":" + keyInformation.TunnelServerPort,
		Remotes:     []string{"R:" + keyInformation.TunnelPort + ":" + "localhost:9001"},
		Fingerprint: keyInformation.TunnelServerFingerprint,
		Auth:        keyInformation.Credentials,
	}

	// TODO: timeout? should stop and error if cannot connect after timeout?
	chiselClient, err := chclient.NewClient(config)
	if err != nil {
		return err
	}

	err = chiselClient.Start(context.Background())
	if err != nil {
		return err
	}
	return nil
}
