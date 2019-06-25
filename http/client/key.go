package client

import (
	"encoding/base64"
	"errors"
	"strings"
)

// parseEdgeKey decodes a base64 encoded key and extract the decoded information from the following
// format: <portainer_instance_url>|<tunnel_server_addr>|<tunnel_server_fingerprint>|<endpoint_id>|<client_credentials>
// <client_credentials> are expected in the user:password format
func parseEdgeKey(key string) (*edgeKey, error) {
	decodedKey, err := base64.RawStdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}

	keyInfo := strings.Split(string(decodedKey), "|")

	if len(keyInfo) != 5 {
		return nil, errors.New("invalid key format")
	}

	edgeKey := &edgeKey{
		PortainerInstanceURL:    keyInfo[0],
		TunnelServerAddr:        keyInfo[1],
		TunnelServerFingerprint: keyInfo[2],
		EndpointID:              keyInfo[3],
		Credentials:             keyInfo[4],
	}

	return edgeKey, nil
}
