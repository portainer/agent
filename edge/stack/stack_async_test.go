package stack

import (
	"context"
	"encoding/base64"
	"math/rand/v2"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"

	"github.com/stretchr/testify/require"
)

func TestPortainerEdgeIDEnvVarPresent(t *testing.T) {
	edgeID := "edge_id"

	sm := NewStackManager(nil, "", nil, edgeID, nil)

	stackID := rand.Int()

	stackPayload := edge.StackPayload{
		ID:   stackID,
		Name: "test-stack",
		DirEntries: []filesystem.DirEntry{
			{
				Name:    "docker-compose.yml",
				Content: base64.StdEncoding.EncodeToString([]byte("test content")),
				IsFile:  true,
			},
		},
		EntryFileName: "docker-compose.yml",
	}

	err := sm.DeployStack(context.Background(), stackPayload)
	require.NoError(t, err)

	edgeStack, ok := sm.stacks[edgeStackID(stackID)]
	require.True(t, ok)
	require.Greater(t, len(edgeStack.EnvVars), 0)
	require.Equal(t, agent.EdgeIdEnvVarName, edgeStack.EnvVars[0].Name)
	require.Equal(t, edgeID, edgeStack.EnvVars[0].Value)
}
