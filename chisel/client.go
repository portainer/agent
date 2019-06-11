package chisel

import (
	"context"
	"encoding/base64"
	"errors"
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

// TODO: change key delimiter ":" to something else, fingerprint uses ":"

// parseEdgeKey decodes a base64 encoded key and extract the decoded information from the following
// format: tunnel_server_addr:tunnel_server_port:tunnel_server_fingerprint:tunnel_port:credentials
// credentials are expected in the user@password format
func parseEdgeKey(key string) (*keyInformation, error) {
	decodedKey, err := base64.RawStdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}

	keyInfo := strings.Split(string(decodedKey), ":")

	// TODO: re-enable after changing delimiter
	//if len(keyInfo) != 5 {
	//	return nil, errors.New("invalid key format")
	//}

	keyInformation := &keyInformation{
		TunnelServerAddr:        keyInfo[0],
		TunnelServerPort:        keyInfo[1],
		TunnelServerFingerprint: keyInfo[3],
		TunnelPort:              keyInfo[2],
		Credentials:             strings.Replace(keyInfo[4], "@", ":", -1),
	}

	return keyInformation, nil
}

func (client *Client) IsKeySet() bool {
	if client.key == nil {
		return false
	}
	return true
}

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
		//log.Fatalf("[ERROR] [edge] [message: Unable to create tunnel client] [error: %s]", err)
	}

	err = chiselClient.Start(context.Background())
	if err != nil {
		return err
		//log.Fatalf("[ERROR] [edge] [message: Unable to start tunnel client] [error: %s]", err)
	}
	return nil
}

//// TODO: should use another options EDGE_KEY and be validated in options parsing
//decodedKey, err := base64.RawStdEncoding.DecodeString(key)
//if err != nil {
//log.Fatalf("[ERROR] - Invalid AGENT_SECRET: %s", err)
//}
//
//keyInfo := strings.Split(string(decodedKey), ":")
//tunnelServerAddr := keyInfo[0]
//tunnelServerPort := keyInfo[1]
//remotePort := keyInfo[2]
//fingerprint := keyInfo[3]
//credentials := strings.Replace(keyInfo[4], "@", ":", -1)
//
//log.Printf("[DEBUG] [edge] [tunnel_server_addr: %s] [tunnel_server_port: %d] [remote_port: %s] [server_fingerprint: %s]", tunnelServerAddr, tunnelServerPort, remotePort, fingerprint)
//
//// TODO: validation must be done somewhere
////or options must be injected
//if tunnelServerAddr == "localhost" {
//if server == "" {
//log.Fatal("[ERROR] - Tunnel server env var required")
//}
//tunnelServerAddr = server
//}
//
//// TODO: manage timeout
//chiselClient, err := chclient.NewClient(&chclient.Config{
//Server:      tunnelServerAddr + ":" + tunnelServerPort,
//Remotes:     []string{"R:" + remotePort + ":" + "localhost:9001"},
//Fingerprint: fingerprint,
//Auth:        credentials,
//})
//if err != nil {
//log.Fatalf("[ERROR] [edge] [message: Unable to create tunnel client] [error: %s]", err)
//}
//
//err = chiselClient.Start(context.Background())
//if err != nil {
//log.Fatalf("[ERROR] [edge] [message: Unable to start tunnel client] [error: %s]", err)
//}
//
//return nil
