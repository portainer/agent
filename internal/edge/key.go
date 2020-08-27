package edge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/client"
)

type edgeKey struct {
	PortainerInstanceURL    string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	EndpointID              string
}

// SetKey parses and associates an Edge key to the agent.
// If the agent is running inside a Swarm cluster, it will also set the "set" flag to specify that a key is set on this agent in the cluster.
func (manager *Manager) SetKey(key string) error {
	edgeKey, err := parseEdgeKey(key)
	if err != nil {
		return err
	}

	err = filesystem.WriteFile(agent.DataDirectory, agent.EdgeKeyFile, []byte(key), 0444)
	if err != nil {
		return err
	}

	manager.key = edgeKey

	if manager.clusterService != nil {
		tags := manager.clusterService.GetRuntimeConfiguration()
		tags.EdgeKeySet = true
		err = manager.clusterService.UpdateRuntimeConfiguration(tags)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetKey returns the Edge key associated to the agent
func (manager *Manager) GetKey() string {
	var encodedKey string

	if manager.key != nil {
		encodedKey = encodeKey(manager.key)
	}

	return encodedKey
}

// IsKeySet returns true if an Edge key is associated to the agent
func (manager *Manager) IsKeySet() bool {
	if manager.key == nil {
		return false
	}
	return true
}

// PropagateKeyInCluster propagates the Edge key associated to the agent to all the other agents inside the cluster
func (manager *Manager) PropagateKeyInCluster() error {
	if manager.clusterService == nil {
		return nil
	}

	httpCli := client.NewAPIClient()
	runtimeConfiguration := manager.clusterService.GetRuntimeConfiguration()
	currentNodeName := runtimeConfiguration.NodeName

	members := manager.clusterService.Members()
	for _, member := range members {

		if member.NodeName == currentNodeName || member.EdgeKeySet {
			continue
		}

		memberAddr := fmt.Sprintf("%s:%s", member.IPAddress, member.Port)

		err := httpCli.SetEdgeKey(memberAddr, manager.GetKey())
		if err != nil {
			return err
		}
	}

	return nil
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
