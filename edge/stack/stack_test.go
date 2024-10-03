package stack

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/internals/mocks"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/assert"
	gomock "go.uber.org/mock/gomock"
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

		mockDeployer.EXPECT().Pull(ctx, stackName, []string{stackFileLocation}, agent.PullOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(nil)

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, portainer.EdgeStackStatusImagesPulled, stack.RollbackTo, "").Return(nil)

		err := manager.pullImages(ctx, stack, stackName, stackFileLocation)
		assert.NoError(t, err)
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

		mockDeployer.EXPECT().Pull(ctx, stackName, []string{stackFileLocation}, agent.PullOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(errors.New("pull failed"))

		err := manager.pullImages(ctx, stack, stackName, stackFileLocation)
		assert.Error(t, err)
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
		assert.NoError(t, err)
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

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, portainer.EdgeStackStatusDeploying, stack.RollbackTo, "").Return(nil)
		mockDeployer.EXPECT().Deploy(ctx, stackName, []string{stackFileLocation}, agent.DeployOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(nil)
		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, portainer.EdgeStackStatusDeploymentReceived, stack.RollbackTo, "").Return(nil)

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

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, portainer.EdgeStackStatusDeploying, stack.RollbackTo, "").Return(nil)
		mockDeployer.EXPECT().Deploy(ctx, stackName, []string{stackFileLocation}, agent.DeployOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
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

		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, portainer.EdgeStackStatusDeploying, stack.RollbackTo, "").Return(nil)
		mockDeployer.EXPECT().Deploy(ctx, stackName, []string{stackFileLocation}, agent.DeployOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		}).Return(errors.New("deploy failed"))
		mockPortainerClient.EXPECT().SetEdgeStackStatus(stack.ID, portainer.EdgeStackStatusError, stack.RollbackTo, "failed to redeploy stack").Return(nil)

		manager.deployStack(ctx, stack, stackName, stackFileLocation)

		assert.Equal(t, StatusError, stack.Status)
		assert.Equal(t, actionIdle, stack.Action)
	})
}

func TestEnvironmentStatusCount(t *testing.T) {
	manager := &StackManager{
		stacks: map[edgeStackID]*edgeStack{1: nil, 2: nil},
	}

	manager.ResetEnvironmentStatusCount()
	assert.Equal(t, manager.resyncdEnvironmentStatusCount, 2)

	isResyncing := manager.isResyncingEnvironmentStatus()
	assert.True(t, isResyncing)

	manager.decrementResyncEnvironmentStatus()
	isResyncing = manager.isResyncingEnvironmentStatus()
	assert.True(t, isResyncing)

	manager.decrementResyncEnvironmentStatus()
	isResyncing = manager.isResyncingEnvironmentStatus()
	assert.False(t, isResyncing)
}

func TestBuildDeployerParams(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Build deployer params for resyncing environment status", func(t *testing.T) {
		mockPortainerClient := mocks.NewMockPortainerClient(ctrl)

		stackPayload := edge.StackPayload{
			ID:            1,
			Version:       1,
			EntryFileName: "stack.yml",
			DirEntries: []filesystem.DirEntry{{
				Name:        "stack.yml",
				Content:     base64.StdEncoding.EncodeToString([]byte(`version: 3.1`)),
				IsFile:      true,
				Permissions: 0755,
			}},
		}

		manager := &StackManager{
			portainerClient: mockPortainerClient,
			stacks:          map[edgeStackID]*edgeStack{1: {StackPayload: stackPayload}},
		}
		manager.ResetEnvironmentStatusCount()
		assert.True(t, manager.isResyncingEnvironmentStatus())

		err := manager.buildDeployerParams(stackPayload, false)
		assert.NoError(t, err)

		stack := manager.stacks[1]
		assert.Equal(t, actionUpdate, stack.Action)

		assert.False(t, manager.isResyncingEnvironmentStatus())
	})
}

func TestProcessStack(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Process stack with resyncing environment status", func(t *testing.T) {
		mockPortainerClient := mocks.NewMockPortainerClient(ctrl)

		stackPayload := edge.StackPayload{
			ID:            1,
			Version:       1,
			EntryFileName: "stack.yml",
			DirEntries: []filesystem.DirEntry{{
				Name:        "stack.yml",
				Content:     base64.StdEncoding.EncodeToString([]byte(`version: 3.1`)),
				IsFile:      true,
				Permissions: 0755,
			}},
		}

		stackStatus := client.StackStatus{Version: 1}

		manager := &StackManager{
			portainerClient: mockPortainerClient,
			stacks:          map[edgeStackID]*edgeStack{1: {StackPayload: stackPayload}},
		}

		mockPortainerClient.EXPECT().GetEdgeStackConfig(1, gomock.Any()).Return(&stackPayload, nil)
		mockPortainerClient.EXPECT().SetEdgeStackStatus(gomock.Any(), gomock.Any(), gomock.Any(), "").Return(nil)

		manager.ResetEnvironmentStatusCount()
		assert.True(t, manager.isResyncingEnvironmentStatus())

		err := manager.processStack(1, stackStatus)
		assert.NoError(t, err)

		stack := manager.stacks[1]
		assert.Equal(t, actionUpdate, stack.Action)

		assert.False(t, manager.isResyncingEnvironmentStatus())
	})
}
