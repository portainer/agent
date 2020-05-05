package edge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
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

	if manager.clusterService != nil {
		tags := manager.clusterService.GetTags()
		tags[agent.MemberTagEdgeKeySet] = "set"
		err = manager.clusterService.UpdateTags(tags)
		if err != nil {
			return err
		}
	}

	return manager.startRuntimeConfigCheckProcess()
}

// GetKey returns the key associated to the manager
func (manager *EdgeManager) GetKey() string {
	var encodedKey string

	if manager.key != nil {
		encodedKey = encodeKey(manager.key)
	}

	return encodedKey
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

func (manager *EdgeManager) retrieveEdgeKey(edgeKey string) (string, error) {

	if edgeKey != "" {
		log.Println("[INFO] [main,edge] [message: Edge key loaded from options]")
		return edgeKey, nil
	}

	var keyRetrievalError error

	edgeKey, keyRetrievalError = retrieveEdgeKeyFromFilesystem()
	if keyRetrievalError != nil {
		return "", keyRetrievalError
	}

	if edgeKey == "" && manager.clusterService != nil {
		edgeKey, keyRetrievalError = retrieveEdgeKeyFromCluster(manager.clusterService)
		if keyRetrievalError != nil {
			return "", keyRetrievalError
		}
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromFilesystem() (string, error) {
	var edgeKey string

	edgeKeyFilePath := fmt.Sprintf("%s/%s", agent.DataDirectory, agent.EdgeKeyFile)

	keyFileExists, err := filesystem.FileExists(edgeKeyFilePath)
	if err != nil {
		return "", err
	}

	if keyFileExists {
		filesystemKey, err := filesystem.ReadFromFile(edgeKeyFilePath)
		if err != nil {
			return "", err
		}

		log.Println("[INFO] [main,edge] [message: Edge key loaded from the filesystem]")
		edgeKey = string(filesystemKey)
	}

	return edgeKey, nil
}

func retrieveEdgeKeyFromCluster(clusterService agent.ClusterService) (string, error) {
	var edgeKey string

	member := clusterService.GetMemberWithEdgeKeySet()
	if member != nil {
		httpCli := client.NewAPIClient()

		memberAddr := fmt.Sprintf("%s:%s", member.IPAddress, member.Port)
		memberKey, err := httpCli.GetEdgeKey(memberAddr)
		if err != nil {
			log.Printf("[ERROR] [main,edge,http,cluster] [message: Unable to retrieve Edge key from cluster member] [error: %s]", err)
			return "", err
		}

		log.Println("[INFO] [main,edge] [message: Edge key loaded from cluster]")
		edgeKey = memberKey
	}

	return edgeKey, nil
}
