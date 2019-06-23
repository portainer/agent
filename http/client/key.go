package client

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
)

// parseEdgeKey decodes a base64 encoded key and extract the decoded information from the following
// format: <portainer_instance_url>|<tunnel_server_port>|<tunnel_server_fingerprint>|<endpoint_id>|<client_credentials>
// <client_credentials> are expected in the user:password format
// The tunnel server address will be created based on the host of the <portainer_instance_url> and the <tunnel_server_port>
// property.
// See more information about the serverAddrOverride parameter in the NewTunnelOperator documentation.
func parseEdgeKey(key, serverAddrOverride string) (*edgeKey, error) {
	decodedKey, err := base64.RawStdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}

	keyInfo := strings.Split(string(decodedKey), "|")

	if len(keyInfo) != 5 {
		return nil, errors.New("invalid key format")
	}

	portainerURL, err := url.Parse(keyInfo[0])
	if err != nil {
		return nil, err
	}

	portainerHost, _, err := net.SplitHostPort(portainerURL.Host)
	if err != nil {
		portainerHost = portainerURL.Host
	}

	portainerInstanceURL := portainerURL.String()

	if portainerHost == "localhost" {
		if serverAddrOverride == "" {
			return nil, errors.New("cannot use localhost as server address")
		}

		portainerHost = serverAddrOverride
		portainerInstanceURL = strings.Replace(portainerInstanceURL, "localhost", serverAddrOverride, 1)
		log.Printf("[DEBUG] [edge,http] [portainer_instance_url: %s] [tunnel_server_host: %s] [message: overriding server address]", portainerInstanceURL, serverAddrOverride)
	}

	edgeKey := &edgeKey{
		PortainerInstanceURL:    portainerInstanceURL,
		TunnelServerAddr:        fmt.Sprintf("%s:%s", portainerHost, keyInfo[1]),
		TunnelServerFingerprint: keyInfo[2],
		EndpointID:              keyInfo[3],
		Credentials:             keyInfo[4],
	}

	return edgeKey, nil
}
