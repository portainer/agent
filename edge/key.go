package edge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
)

type edgeKey struct {
	PortainerInstanceURL    string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	EndpointID              string
}

// SetKey parses and associate a key to the manager
func (manager *EdgeManager) SetKey(key string) error {
	edgeKey, err := parseEdgeKey(key)
	if err != nil {
		return err
	}

	err = filesystem.WriteFile(agent.DataDirectory, agent.EdgeKeyFile, []byte(key), 0444)
	if err != nil {
		return err
	}

	manager.key = edgeKey

	return nil
}

// GetKey returns the key associated to the manager
func (manager *EdgeManager) GetKey() string {
	var encodedKey string

	if manager.key != nil {
		encodedKey = encodeKey(manager.key)
	}

	return encodedKey
}

// GetPortainerConfig returns portainer url and endpoint id
func (manager *EdgeManager) GetPortainerConfig() (string, string, error) {
	if manager.key == nil {
		return "", "", errors.New("Key is not set")
	}

	key := manager.key
	return key.PortainerInstanceURL, key.EndpointID, nil
}

// GetTunnelConfig returns tunnel url and tunnel fingerprint
func (manager *EdgeManager) GetTunnelConfig() (string, string, error) {
	if manager.key == nil {
		return "", "", errors.New("Key is not set")
	}

	key := manager.key
	return key.TunnelServerAddr, key.TunnelServerFingerprint, nil
}

// IsKeySet checks if a key is associated to the manager
func (manager *EdgeManager) IsKeySet() bool {
	if manager.key == nil {
		return false
	}
	return true
}

// parseEdgeKey decodes a base64 encoded key and extract the decoded information from the following
// format: <portainer_instance_url>|<tunnel_server_addr>|<tunnel_server_fingerprint>|<endpoint_id>
// <client_credentials> are expected in the user:password format
func parseEdgeKey(key string) (*edgeKey, error) {
	decodedKey, err := base64.RawStdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}

	keyInfo := strings.Split(string(decodedKey), "|")

	if len(keyInfo) != 4 {
		return nil, errors.New("invalid key format")
	}

	edgeKey := &edgeKey{
		PortainerInstanceURL:    keyInfo[0],
		TunnelServerAddr:        keyInfo[1],
		TunnelServerFingerprint: keyInfo[2],
		EndpointID:              keyInfo[3],
	}

	return edgeKey, nil
}

func encodeKey(edgeKey *edgeKey) string {
	keyInfo := fmt.Sprintf("%s|%s|%s|%s", edgeKey.PortainerInstanceURL, edgeKey.TunnelServerAddr, edgeKey.TunnelServerFingerprint, edgeKey.EndpointID)
	encodedKey := base64.RawStdEncoding.EncodeToString([]byte(keyInfo))
	return encodedKey
}
