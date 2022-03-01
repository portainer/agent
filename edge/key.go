package edge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/filesystem"
)

type edgeKey struct {
	PortainerInstanceURL    string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	EndpointID              string
}

// SetKey parses and associates an Edge key to the agent.
// If the agent is running inside a cluster, it will also set the "set" flag to specify that a key is set on this agent in the cluster.
func (manager *Manager) SetKey(key string, persist bool) error {
	edgeKey, err := parseEdgeKey(key)
	if err != nil {
		return err
	}

	if persist {
		err = filesystem.WriteFile(manager.agentOptions.DataPath, agent.EdgeKeyFile, []byte(key), 0444)
		if err != nil {
			return err
		}
	}

	manager.key = edgeKey

	if manager.clusterService != nil {
		tags := manager.clusterService.GetRuntimeConfiguration()
		tags.EdgeKeySet = true
		return manager.clusterService.UpdateRuntimeConfiguration(tags)
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
	return manager.key != nil
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

func RetrieveEdgeKey(dataPath string, edgeKey string, clusterService agent.ClusterService) (string, error) {
	if edgeKey != "" {
		log.Println("[INFO] [edge] [message: Edge key loaded from options]")
		return edgeKey, nil
	}

	var keyRetrievalError error

	edgeKey, keyRetrievalError = retrieveEdgeKeyFromFilesystem(dataPath)
	if keyRetrievalError != nil {
		return "", keyRetrievalError
	}

	if edgeKey == "" && clusterService != nil {
		edgeKey, keyRetrievalError = retrieveEdgeKeyFromCluster(clusterService)
		if keyRetrievalError != nil {
			return "", keyRetrievalError
		}
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromFilesystem(dataPath string) (string, error) {
	edgeKeyFilePath := fmt.Sprintf("%s/%s", dataPath, agent.EdgeKeyFile)

	keyFileExists, err := filesystem.FileExists(edgeKeyFilePath)
	if err != nil {
		return "", err
	}

	if !keyFileExists {
		return "", nil
	}

	filesystemKey, err := filesystem.ReadFromFile(edgeKeyFilePath)
	if err != nil {
		return "", err
	}

	log.Println("[INFO] [edge] [message: Edge key loaded from the filesystem]")
	return string(filesystemKey), nil
}

func retrieveEdgeKeyFromCluster(clusterService agent.ClusterService) (string, error) {
	member := clusterService.GetMemberWithEdgeKeySet()
	if member == nil {
		return "", nil
	}

	httpCli := client.NewAPIClient()

	memberAddr := fmt.Sprintf("%s:%s", member.IPAddress, member.Port)
	memberKey, err := httpCli.GetEdgeKey(memberAddr)
	if err != nil {
		log.Printf("[ERROR] [edge] [message: Unable to retrieve Edge key from cluster member] [error: %s]", err)
		return "", err
	}

	log.Println("[INFO] [edge] [message: Edge key loaded from cluster]")
	return memberKey, nil
}
