package stack

import (
	"context"
	"errors"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/internals/mocks"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
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

		mockPortainerClient.EXPECT().SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusImagesPulled, stack.RollbackTo, "").Return(nil)

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
