package stack

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	"github.com/portainer/agent/edge/aws"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/internals/mocks"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/libstack"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStackManager_pullImages(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeployer := mocks.NewMockDeployer(ctrl)
	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)

	manager := &StackManager{
		deployer:        mockDeployer,
		portainerClient: mockPortainerClient,
	}

	t.Run("Pull images successfully", func(t *testing.T) {
		stack := &edgeStack{
			PullCount:    0,
			Status:       StatusPending,
			PullFinished: false,
			FileFolder:   "/path/to/stack",
			StackPayload: edge.StackPayload{
				PrePullImage: true,
			},
		}

		ctx := context.Background()
		stackName := "my-stack"
		stackFileLocation := "/path/to/stack/stack.yml"

		mockDeployer.EXPECT().Pull(ctx, stackName, []string{stackFileLocation}, deployer.PullOptions{
			DeployerBaseOptions: deployer.DeployerBaseOptions{
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(nil)

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, portainer.EdgeStackStatusImagesPulled, stack.RollbackTo, "").Return(nil)

		err := manager.pullImages(ctx, stack, stackName, stackFileLocation)
		require.NoError(t, err)
		assert.True(t, stack.PullFinished)
		assert.Equal(t, StatusDeploying, stack.Status)
	})

	t.Run("Pull images failed with retries", func(t *testing.T) {
		stack := &edgeStack{
			PullCount:    0,
			Status:       StatusPending,
			PullFinished: false,
			FileFolder:   "/path/to/stack",
			StackPayload: edge.StackPayload{
				PrePullImage: true,
			},
		}

		ctx := context.Background()
		stackName := "my-stack"
		stackFileLocation := "/path/to/stack/stack.yml"

		mockDeployer.EXPECT().Pull(ctx, stackName, []string{stackFileLocation}, deployer.PullOptions{
			DeployerBaseOptions: deployer.DeployerBaseOptions{
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(errors.New("pull failed"))

		err := manager.pullImages(ctx, stack, stackName, stackFileLocation)
		require.Error(t, err)
		assert.False(t, stack.PullFinished)
		assert.Equal(t, StatusRetry, stack.Status)
	})

	t.Run("Skip pulling images", func(t *testing.T) {
		stack := &edgeStack{
			PullCount:    0,
			Status:       StatusPending,
			PullFinished: false,
			FileFolder:   "/path/to/stack",
			StackPayload: edge.StackPayload{},
		}

		ctx := context.Background()
		stackName := "my-stack"
		stackFileLocation := "/path/to/stack/stack.yml"

		stack.PullCount = perHourRetries + 1

		err := manager.pullImages(ctx, stack, stackName, stackFileLocation)
		require.NoError(t, err)
		assert.False(t, stack.PullFinished)
		assert.Equal(t, StatusPending, stack.Status)
	})
}

func TestStackManager_deployStack(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeployer := mocks.NewMockDeployer(ctrl)
	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)

	manager := &StackManager{
		deployer:        mockDeployer,
		portainerClient: mockPortainerClient,
	}

	t.Run("Deploy stack successfully", func(t *testing.T) {
		ctx := context.Background()
		stack := &edgeStack{
			DeployCount: 0,
			Status:      StatusPending,
			FileFolder:  "/path/to/stack",
			Action:      actionIdle,

			StackPayload: edge.StackPayload{
				ID:          1,
				RetryDeploy: true,
				Namespace:   "default",
				EnvVars:     []portainer.Pair{{Name: "key", Value: "value"}},
				Version:     1,
			},
		}

		stackName := "my-stack"
		stackFileLocation := "/path/to/stack/stack.yml"

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, portainer.EdgeStackStatusDeploying, stack.RollbackTo, "").Return(nil)
		mockDeployer.EXPECT().Deploy(ctx, stackName, []string{stackFileLocation}, deployer.DeployOptions{
			EdgeStackID: portainer.EdgeStackID(stack.ID),
			DeployerBaseOptions: deployer.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(nil)
		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, portainer.EdgeStackStatusDeploymentReceived, stack.RollbackTo, "").Return(nil)

		manager.deployStack(ctx, stack, stackName, stackFileLocation)

		assert.Equal(t, StatusAwaitingDeployedStatus, stack.Status)
		assert.Equal(t, actionIdle, stack.Action)
	})

	t.Run("Deploy stack failed with retries", func(t *testing.T) {
		ctx := context.Background()
		stack := &edgeStack{
			DeployCount: 0,
			Status:      StatusPending,
			FileFolder:  "/path/to/stack",
			Action:      actionIdle,

			StackPayload: edge.StackPayload{
				ID:          1,
				RetryDeploy: true,
				Namespace:   "default",
				EnvVars:     []portainer.Pair{{Name: "key", Value: "value"}},
				Version:     1,
			},
		}

		stackName := "my-stack"
		stackFileLocation := "/path/to/stack/stack.yml"

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, portainer.EdgeStackStatusDeploying, stack.RollbackTo, "").Return(nil)
		mockDeployer.EXPECT().Deploy(ctx, stackName, []string{stackFileLocation}, deployer.DeployOptions{
			EdgeStackID: portainer.EdgeStackID(stack.ID),
			DeployerBaseOptions: deployer.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(errors.New("deploy failed"))

		manager.deployStack(ctx, stack, stackName, stackFileLocation)

		assert.Equal(t, StatusRetry, stack.Status)
		assert.Equal(t, actionIdle, stack.Action)
	})

	t.Run("Deploy stack failed without retries", func(t *testing.T) {
		ctx := context.Background()
		stack := &edgeStack{
			DeployCount: 0,
			Status:      StatusPending,
			FileFolder:  "/path/to/stack",
			Action:      actionIdle,

			StackPayload: edge.StackPayload{
				ID:          1,
				RetryDeploy: false,
				Namespace:   "default",
				EnvVars:     []portainer.Pair{{Name: "key", Value: "value"}},
				Version:     1,
			},
		}

		stackName := "my-stack"
		stackFileLocation := "/path/to/stack/stack.yml"

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, portainer.EdgeStackStatusDeploying, stack.RollbackTo, "").Return(nil)
		mockDeployer.EXPECT().Deploy(ctx, stackName, []string{stackFileLocation}, deployer.DeployOptions{
			EdgeStackID: portainer.EdgeStackID(stack.ID),
			DeployerBaseOptions: deployer.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(errors.New("deploy failed"))
		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, portainer.EdgeStackStatusError, stack.RollbackTo, "failed to redeploy stack: deploy failed").Return(nil)

		manager.deployStack(ctx, stack, stackName, stackFileLocation)

		assert.Equal(t, StatusError, stack.Status)
		assert.Equal(t, actionIdle, stack.Action)
	})
}

func TestStackManager_checkStackStatus(t *testing.T) {
	tests := []struct {
		name                           string
		edgeUpdateID                   int
		stackStatus                    edgeStackStatus
		expectedRequiredLibStackStatus libstack.Status
		expectedWaitResult             libstack.WaitResult
		expectedEdgeStackStatus        edgeStackStatus
		expectedPortainerStatus        portainer.EdgeStackStatusType
	}{
		{
			name:                           "EdgeStack StatusDeployed -> StatusCompleted (Completed)",
			stackStatus:                    StatusDeployed,
			expectedRequiredLibStackStatus: libstack.StatusCompleted,
			expectedWaitResult: libstack.WaitResult{
				Status: libstack.StatusCompleted,
			},
			expectedEdgeStackStatus: StatusCompleted,
			expectedPortainerStatus: portainer.EdgeStackStatusCompleted,
		},
		{
			name:                           "EdgeStack StatusDeployed -> StatusCompleted (Error)",
			stackStatus:                    StatusDeployed,
			expectedRequiredLibStackStatus: libstack.StatusCompleted,
			expectedWaitResult: libstack.WaitResult{
				Status:   libstack.StatusError,
				ErrorMsg: "test " + context.DeadlineExceeded.Error(),
			},
			expectedEdgeStackStatus: StatusDeployed, // If an error occurs, we don't update the status
			expectedPortainerStatus: -1,             // No update to Portainer
		},
		{
			name:                           "EdgeStack StatusDeployed -> StatusError (context deadline exceeded)",
			stackStatus:                    StatusDeployed,
			expectedRequiredLibStackStatus: libstack.StatusCompleted,
			expectedWaitResult: libstack.WaitResult{
				Status:   libstack.StatusError,
				ErrorMsg: "test " + context.DeadlineExceeded.Error(), // if the context deadline is exceeded, the status is not updated
			},
			expectedEdgeStackStatus: StatusDeployed,
			expectedPortainerStatus: -1, // No update to Portainer
		},
		{
			name:                           "EdgeStack StatusAwaitingDeployedStatus -> StatusDeployed (Running)",
			stackStatus:                    StatusAwaitingDeployedStatus,
			expectedRequiredLibStackStatus: libstack.StatusRunning,
			expectedWaitResult: libstack.WaitResult{
				Status: libstack.StatusRunning,
			},
			expectedEdgeStackStatus: StatusDeployed,
			expectedPortainerStatus: portainer.EdgeStackStatusRunning,
		},
		{
			name:                           "EdgeStack StatusAwaitingRemovedStatus -> StatusAwaitingCleanup (removed)",
			stackStatus:                    StatusAwaitingRemovedStatus,
			expectedRequiredLibStackStatus: libstack.StatusRemoved,
			expectedWaitResult: libstack.WaitResult{
				Status: libstack.StatusRemoved,
			},
			expectedEdgeStackStatus: StatusAwaitingCleanup,
			expectedPortainerStatus: portainer.EdgeStackStatusRemoved,
		},
		{
			name:                           "EdgeUpdate StatusDeployed -> StatusCompleted (StatusCompleted)",
			edgeUpdateID:                   1,
			stackStatus:                    StatusDeployed,
			expectedRequiredLibStackStatus: libstack.StatusCompleted,
			expectedWaitResult: libstack.WaitResult{
				Status:   libstack.StatusCompleted,
				ErrorMsg: "",
			},
			expectedEdgeStackStatus: StatusCompleted,
			expectedPortainerStatus: portainer.EdgeStackStatusCompleted,
		},
		{
			name:                           "EdgeUpdate StatusDeployed -> StatusError (context deadline exceeded)",
			edgeUpdateID:                   1,
			stackStatus:                    StatusDeployed,
			expectedRequiredLibStackStatus: libstack.StatusCompleted,
			expectedWaitResult: libstack.WaitResult{
				Status:   libstack.StatusError,
				ErrorMsg: "test " + context.DeadlineExceeded.Error(), // if the context deadline is exceeded, the status is not updated
			},
			expectedEdgeStackStatus: StatusDeployed,
			expectedPortainerStatus: -1, // No update to Portainer
		},
		{
			name:                           "EdgeUpdate StatusDeployed -> StatusError (error)",
			edgeUpdateID:                   1,
			stackStatus:                    StatusDeployed,
			expectedRequiredLibStackStatus: libstack.StatusCompleted,
			expectedWaitResult: libstack.WaitResult{
				Status:   libstack.StatusError,
				ErrorMsg: "test-error", // the edge update updater failed
			},
			expectedEdgeStackStatus: StatusError,
			expectedPortainerStatus: portainer.EdgeStackStatusError,
		},
		{
			name:                           "EdgeUpdate StatusAwaitingDeployedStatus -> StatusDeployed (Running)",
			edgeUpdateID:                   1,
			stackStatus:                    StatusAwaitingDeployedStatus,
			expectedRequiredLibStackStatus: libstack.StatusRunning,
			expectedWaitResult: libstack.WaitResult{
				Status:   libstack.StatusRunning,
				ErrorMsg: "",
			},
			expectedEdgeStackStatus: StatusDeployed,
			expectedPortainerStatus: portainer.EdgeStackStatusRunning,
		},
		{
			name:                           "EdgeUpdate StatusAwaitingRemovedStatus -> StatusAwaitingCleanup (removed)",
			stackStatus:                    StatusAwaitingRemovedStatus,
			expectedRequiredLibStackStatus: libstack.StatusRemoved,
			expectedWaitResult: libstack.WaitResult{
				Status: libstack.StatusRemoved,
			},
			expectedEdgeStackStatus: StatusAwaitingCleanup,
			expectedPortainerStatus: portainer.EdgeStackStatusRemoved,
		},
		{
			name:                           "StatusAwaitingDeployedStatus -> Unknown",
			stackStatus:                    StatusAwaitingDeployedStatus,
			expectedRequiredLibStackStatus: libstack.StatusRunning,
			expectedWaitResult: libstack.WaitResult{
				Status: libstack.StatusUnknown,
			},
			expectedEdgeStackStatus: StatusAwaitingDeployedStatus,
			expectedPortainerStatus: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockDeployer := mocks.NewMockDeployer(ctrl)
			mockPortainerClient := mocks.NewMockPortainerClient(ctrl)

			manager := &StackManager{
				deployer:        mockDeployer,
				portainerClient: mockPortainerClient,
			}
			stack := &edgeStack{
				Status: tt.stackStatus,
				StackPayload: edge.StackPayload{
					Name:         "edge-stack",
					EdgeUpdateID: tt.edgeUpdateID,
				},
			}
			ctx := context.Background()

			mockDeployer.EXPECT().WaitForStatus(gomock.Any(), stack.Name, tt.expectedRequiredLibStackStatus, deployer.CheckStatusOptions{}).Return(tt.expectedWaitResult)
			if tt.expectedPortainerStatus >= 0 {
				mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, stack.Version, tt.expectedPortainerStatus, stack.RollbackTo, tt.expectedWaitResult.ErrorMsg).Return(nil)
			}

			err := manager.checkStackStatus(ctx, stack.Name, stack, deployer.CheckStatusOptions{})

			require.NoError(t, err)
			assert.Equal(t, tt.expectedEdgeStackStatus, stack.Status)
		})
	}
}

type mockPortainerClient struct {
	client.PortainerClient
	t            *testing.T
	stackPayload *edge.StackPayload
}

func (m *mockPortainerClient) SetEdgeStackStatus(
	edgeStackID,
	version int,
	edgeStackStatus portainer.EdgeStackStatusType,
	rollbackTo *int,
	errMessage string,
) error {
	if edgeStackStatus == portainer.EdgeStackStatusError {
		m.t.Fatal(errMessage)
	}
	return nil
}

func (m *mockPortainerClient) GetEdgeStackConfig(stackID int, version *int) (*edge.StackPayload, error) {
	if m.stackPayload != nil {
		return m.stackPayload, nil
	}
	return &edge.StackPayload{}, nil
}

func setupStackManager(t *testing.T) *StackManager {
	// Create a compose file
	composeFile := `services:
		nginx:
			image: nginx`

	stackPayload := &edge.StackPayload{
		ID:   1,
		Name: "test-stack",
		DirEntries: []filesystem.DirEntry{
			{
				Name:    "docker-compose.yml",
				Content: base64.StdEncoding.EncodeToString([]byte(composeFile)),
				IsFile:  true,
			},
		},
		Version:       1,
		EntryFileName: "docker-compose.yml",
	}

	stackFolder := getStackFileFolder(&edgeStack{StackPayload: *stackPayload})
	require.NoError(t, os.MkdirAll(stackFolder, 0755))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(stackFolder))
	})

	// Mock portainer client
	mockClient := &mockPortainerClient{stackPayload: stackPayload}

	manager := NewStackManager(mockClient, nil, "edge_id", nil)
	manager.stacks[edgeStackID(stackPayload.ID)] = &edgeStack{
		StackPayload: edge.StackPayload{
			ID: stackPayload.ID, Version: stackPayload.Version,
		},
	}

	return manager
}

func TestStackManager_processStack_ForceRecreate(t *testing.T) {
	t.Run("Force redeploy flag - should set ForceRecreate to true", func(t *testing.T) {
		manager := setupStackManager(t)
		stackStatus := client.StackStatus{Version: 1, ForceRedeploy: true}

		// Test the target function
		require.NoError(t, manager.processStack(1, stackStatus))

		// Verify the stack was created and ForceRecreate is set to true
		stack, exists := manager.stacks[edgeStackID(1)]
		require.True(t, exists)
		require.True(t, stack.DeployerOptionsPayload.ForceRecreate)
	})

	t.Run("No force flags - should set ForceRecreate to false", func(t *testing.T) {
		manager := setupStackManager(t)
		stackStatus := client.StackStatus{Version: 1}

		// Test the target function
		require.NoError(t, manager.processStack(1, stackStatus))

		// Verify the stack was created and ForceRecreate is set to false
		stack, exists := manager.stacks[edgeStackID(1)]
		require.True(t, exists)
		require.False(t, stack.DeployerOptionsPayload.ForceRecreate)
	})
}

func TestResetForceRecreateStatus(t *testing.T) {
	t.Run("ForceRecreate is true", func(t *testing.T) {
		stack := &edgeStack{
			StackPayload: edge.StackPayload{
				DeployerOptionsPayload: edge.DeployerOptionsPayload{
					ForceRecreate: true,
				},
			},
		}
		resetForceRecreateStatus(stack)
		require.False(t, stack.DeployerOptionsPayload.ForceRecreate)
	})
	t.Run("ForceRecreate is false", func(t *testing.T) {
		stack := &edgeStack{
			StackPayload: edge.StackPayload{
				DeployerOptionsPayload: edge.DeployerOptionsPayload{
					ForceRecreate: false,
				},
			},
		}
		resetForceRecreateStatus(stack)
		require.False(t, stack.DeployerOptionsPayload.ForceRecreate)
	})
}

func TestStackManager_performActionOnStack(t *testing.T) {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)

	cli := &mockPortainerClient{t: t}

	edgeID := "test-edge"

	manager := NewStackManager(cli, nil, edgeID, nil)

	if err := manager.SetEngineType(EngineTypeDockerStandalone); err != nil {
		t.Fatal("setting manager engine type: ", err)
	}

	dir := t.TempDir()

	dirEntries := []filesystem.DirEntry{
		{
			Name: "docker-compose.yml",
			Content: `services:
  test:
    image: busybox:latest
    stop_signal: SIGKILL
    command: tail -f /dev/null
    environment:
      - OTHER_VAR=$OTHER_VAR
      - PORTAINER_HOST_VAR=$PORTAINER_HOST_VAR
      - HOST_VAR=$HOST_VAR`,
			IsFile:      true,
			Permissions: 0644,
		},
	}

	if err := filesystem.PersistDir(dir, dirEntries); err != nil {
		t.Fatal("Failed to create compose dir: ", err)
	}

	stack := &edgeStack{
		StackPayload: edge.StackPayload{
			Version:             1,
			ID:                  1,
			Name:                "test-env-vars",
			DirEntries:          dirEntries,
			EntryFileName:       "docker-compose.yml",
			SupportRelativePath: false,
			FilesystemPath:      dir,
			EnvVars: []portainer.Pair{{
				Name:  "OTHER_VAR",
				Value: "test",
			}},
			DeployerOptionsPayload: edge.DeployerOptionsPayload{},
		},
		FileFolder:   dir,
		FileName:     "docker-compose.yml",
		Status:       StatusPending,
		Action:       actionDeploy,
		PullCount:    0,
		PullFinished: false,
		DeployCount:  0,
		FirstAction:  time.Now().Add(time.Duration(-10) * time.Minute),
		LastAction:   time.Now().Add(time.Duration(-10) * time.Minute),
	}

	manager.stacks[edgeStackID(stack.ID)] = stack

	t.Setenv("HOST_VAR", "something")
	t.Setenv("PORTAINER_HOST_VAR", "hello")

	manager.performActionOnStack()

	t.Cleanup(func() {
		stack.Status = StatusPending
		stack.Action = actionDelete
		stack.LastAction = time.Now().Add(time.Duration(-10) * time.Minute)
		manager.performActionOnStack()
	})

	containerName := "edge_" + stack.Name + "-test-" + strconv.Itoa(stack.Version)
	require.True(t, containerExists(containerName))

	env := getContainerEnv(containerName)

	require.Empty(t, env["HOST_VAR"], "HOST_VAR env var should not be set in created container")
	require.Equal(t, "hello", env["PORTAINER_HOST_VAR"], "PORTAINER_HOST_VAR env var should be set in created container")
	require.Equal(t, "test", env["OTHER_VAR"], "OTHER_VAR env var should be set in created container")
}

func TestStackManager_performActionOnStack_EdgeUpdateScenarios(t *testing.T) {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)

	tests := []struct {
		name            string
		stack           *edgeStack
		waitResult      libstack.WaitResult
		expectStatus    edgeStackStatus
		expectSetStatus bool
		expectErrorMsg  string
	}{
		{
			name: "Timeout on running edge update should not change status",
			stack: &edgeStack{
				StackPayload: edge.StackPayload{
					ID:           1,
					Name:         "test",
					EdgeUpdateID: 1,
				},
				Status: StatusDeployed,
				Action: actionIdle,
			},
			waitResult: libstack.WaitResult{
				Status:   libstack.StatusError,
				ErrorMsg: context.DeadlineExceeded.Error(),
			},
			expectStatus:    StatusDeployed,
			expectSetStatus: false,
		},
		{
			name: "Failed edge update should set error status to prevent stack from being redeployed",
			stack: &edgeStack{
				StackPayload: edge.StackPayload{
					ID:           1,
					Name:         "test",
					EdgeUpdateID: 1,
				},
				EdgeUpdateFailed: true,
				Status:           StatusPending,
				Action:           actionDeploy,
			},
			waitResult: libstack.WaitResult{
				Status:   libstack.StatusError,
				ErrorMsg: "edge update failed",
			},
			expectStatus:    StatusError,
			expectSetStatus: true,
			expectErrorMsg:  "edge update failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockDeployer := mocks.NewMockDeployer(ctrl)
			mockDeployer.EXPECT().
				WaitForStatus(gomock.Any(), "edge_test", libstack.StatusCompleted, gomock.AssignableToTypeOf(deployer.CheckStatusOptions{})).
				Return(tt.waitResult)

			mockClient := mocks.NewMockPortainerClient(ctrl)

			if tt.expectSetStatus {
				mockClient.EXPECT().
					SetEdgeStackStatus(tt.stack.ID, tt.stack.Version, portainer.EdgeStackStatusError, tt.stack.RollbackTo, tt.expectErrorMsg).
					Return(nil)
			}

			manager := NewStackManager(mockClient, nil, "test", nil)
			manager.deployer = mockDeployer
			manager.stacks[edgeStackID(tt.stack.ID)] = tt.stack

			manager.performActionOnStack()

			updated := manager.stacks[edgeStackID(tt.stack.ID)]
			assert.Equal(t, tt.expectStatus, updated.Status)
		})
	}
}

func containerExists(containerName string) bool {
	cmd := exec.Command("docker", "ps", "-a", "-f", "name="+containerName)

	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("failed to list containers: %s", err)
	}

	return strings.Contains(string(out), containerName)
}

func getContainerEnv(containerName string) map[string]string {
	cmd := exec.Command("docker", "exec", containerName, "env")

	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("failed to list containers: %s", err)
	}

	vars := map[string]string{}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		k, v, found := strings.Cut(scanner.Text(), "=")
		if found {
			vars[k] = v
		}
	}

	return vars
}

func TestAddRegistryToEntryFile_Docker(t *testing.T) {
	t.Parallel()
	manager := NewStackManager(nil, nil, "edge_id", nil)
	manager.engineType = EngineTypeDockerStandalone // directly set to avoid deployer setup

	composeContent := `version: "3"
services:
  updater:
    image: myregistry.local/portainer/updater:latest
`

	expectedUsername := "user"
	expectedPassword := "secret"
	stackPayload := edge.StackPayload{
		ID:            42,
		Name:          "test-registry",
		EntryFileName: filesystem.ComposeFileDefaultName,
		EdgeUpdateID:  1, // required to trigger credentials injection
		DirEntries: []filesystem.DirEntry{
			{
				Name:    filesystem.ComposeFileDefaultName,
				IsFile:  true,
				Content: composeContent,
			},
		},
		RegistryCredentials: []edge.RegistryCredentials{
			{
				ServerURL: "myregistry.local",
				Username:  expectedUsername,
				Secret:    expectedPassword,
			},
		},
	}

	err := manager.addRegistryToEntryFile(&stackPayload)
	require.NoError(t, err)
	require.Nil(t, manager.awsConfig, "awsConfig should be nil to ensure AWS path not used")

	var registryUsername, registryPassword string
	for _, p := range stackPayload.EnvVars {
		if p.Name == "REGISTRY_USERNAME" {
			registryUsername = p.Value
		}
		if p.Name == "REGISTRY_PASSWORD" {
			registryPassword = p.Value
		}
	}
	require.Equal(t, expectedUsername, registryUsername)
	require.Equal(t, expectedPassword, registryPassword)

	updatedContent := stackPayload.DirEntries[0].Content
	require.Contains(t, updatedContent, "REGISTRY_USED=1")
	require.Contains(t, updatedContent, "REGISTRY_USERNAME=${REGISTRY_USERNAME}")
	require.Contains(t, updatedContent, "REGISTRY_PASSWORD=${REGISTRY_PASSWORD}")
}

func TestEnsureRegCreds(t *testing.T) {
	manager := setupStackManager(t)

	t.Run("standard credentials get added", func(t *testing.T) {
		stack := &edgeStack{
			StackPayload: edge.StackPayload{
				RegistryCredentials: []edge.RegistryCredentials{
					{
						ServerURL: "https://registry.example.com",
						Username:  "username",
						Secret:    "secret",
					},
				},
			},
		}

		rcs, err := manager.ensureRegCreds(stack)
		require.NoError(t, err)
		require.Len(t, rcs, 1)
		require.Equal(t, "username", rcs[0].Username)
		require.Equal(t, "secret", rcs[0].Secret)
	})

	awsConfig := &agent.AWSConfig{
		RoleARN:        "role",
		TrustAnchorARN: "trust-anchor",
		ProfileARN:     "profile",
		Region:         "us-east-1",
	}

	t.Run("aws config but not private ECR repository", func(t *testing.T) {
		manager.awsConfig = awsConfig
		stack := &edgeStack{
			StackPayload: edge.StackPayload{
				RegistryCredentials: []edge.RegistryCredentials{
					{
						ServerURL: "https://registry.example.com",
					},
					{
						ServerURL: "public.ecr.aws/my-repo",
					},
					{
						ServerURL: "docker.io/library/ubuntu:latest",
					},
				},
			},
		}

		// invalid credentials, but not a private ECR repository, so IAMRA should be skipped
		rcs, err := manager.ensureRegCreds(stack)
		require.NoError(t, err)
		require.Empty(t, rcs)
	})

	t.Run("aws config and private ECR repository fails", func(t *testing.T) {
		manager.awsConfig = awsConfig
		stack := &edgeStack{
			StackPayload: edge.StackPayload{
				RegistryCredentials: []edge.RegistryCredentials{
					{
						ServerURL: "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo:foo",
					},
				},
			},
		}

		// stop retrying the IAMRA login for this test
		maxRetries := aws.RetrySettings.MaxRetries
		defer func() {
			aws.RetrySettings.MaxRetries = maxRetries
		}()
		aws.RetrySettings.MaxRetries = 1

		// the test does not have valid credentials, this is replicating a situation that should not continue since a
		// private ECR repo is required but it is impossible to get valid credentials
		_, err := manager.ensureRegCreds(stack)
		require.Error(t, err)
	})
}

func TestStackManager_processStack_resetsFirstActionOnUpdate(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)

	stackID := 42
	helmPayload := &edge.StackPayload{
		ID:      stackID,
		Version: 2,
		HelmConfig: portainer.HelmConfig{
			ChartPath: "nginx",
		},
	}

	manager := &StackManager{
		stacks:          map[edgeStackID]*edgeStack{},
		portainerClient: mockPortainerClient,
	}

	manager.stacks[edgeStackID(stackID)] = &edgeStack{
		StackPayload: edge.StackPayload{
			ID:      stackID,
			Version: 1,
		},
		FirstAction: time.Now().Add(-5 * time.Minute),
	}

	mockPortainerClient.EXPECT().
		GetEdgeStackConfig(stackID, gomock.Any()).
		Return(helmPayload, nil)

	mockPortainerClient.EXPECT().
		SetEdgeStackStatus(stackID, 2, portainer.EdgeStackStatusAcknowledged, gomock.Any(), "").
		Return(nil)

	stackStatus := client.StackStatus{
		ID:      stackID,
		Version: 2,
	}

	err := manager.processStack(stackID, stackStatus)
	require.NoError(t, err)

	stack := manager.stacks[edgeStackID(stackID)]
	require.True(t, stack.FirstAction.IsZero())
}
