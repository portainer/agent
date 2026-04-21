package stack

import (
	"context"
	"encoding/base64"
	"math/rand/v2"
	"os"
	"testing"
	"time"

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

func setupManagerAndFile(t *testing.T) (*StackManager, string, int) {
	stackID := rand.Int()

	manager := NewStackManager(nil, "", nil, "edge_id", nil)
	manager.stacks[edgeStackID(stackID)] = &edgeStack{
		StackPayload: edge.StackPayload{ID: stackID, Version: 1},
	}

	composeFile := `services:
		nginx:
			image: nginx`

	stackFolder := getStackFileFolder(&edgeStack{StackPayload: edge.StackPayload{ID: stackID, Version: 1}})
	require.NoError(t, os.MkdirAll(stackFolder, 0755))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(stackFolder))
	})

	return manager, composeFile, stackID
}

func TestStack_BuildDeployerParams_resetsFirstAction(t *testing.T) {
	t.Parallel()
	manager, composeFile, stackID := setupManagerAndFile(t)

	manager.stacks[edgeStackID(stackID)].FirstAction = time.Now().Add(-5 * time.Minute)

	stackPayload := edge.StackPayload{
		ID:      stackID,
		Version: 2,
		DirEntries: []filesystem.DirEntry{
			{
				Name:    "docker-compose.yml",
				Content: base64.StdEncoding.EncodeToString([]byte(composeFile)),
				IsFile:  true,
			},
		},
		EntryFileName: "docker-compose.yml",
	}

	err := manager.buildDeployerParams(stackPayload, false)
	require.NoError(t, err)

	stack := manager.stacks[edgeStackID(stackID)]
	require.True(t, stack.FirstAction.IsZero())
}
