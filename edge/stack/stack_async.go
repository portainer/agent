package stack

import (
	"context"

	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/rs/zerolog/log"
)

func (manager *StackManager) DeleteNormalStack(ctx context.Context, stackName string, removeVolumes bool) error {
	log.Debug().Str("stack_name", stackName).Msg("removing normal stack")

	if err := manager.deployer.Remove(ctx, stackName, []string{}, deployer.RemoveOptions{Volumes: removeVolumes}); err != nil {
		log.Error().Err(err).Msg("unable to remove normal stack")

		return err
	}

	return nil
}

func (manager *StackManager) DeployStack(ctx context.Context, stackData edge.StackPayload) error {
	return manager.buildDeployerParams(stackData, false)
}

func (manager *StackManager) DeleteStack(ctx context.Context, stackData edge.StackPayload) error {
	return manager.buildDeployerParams(stackData, true)
}

func (manager *StackManager) buildDeployerParams(stackPayload edge.StackPayload, deleteStack bool) error {
	var stack *edgeStack

	// The stack information will be shared with edge agent registry server (request by docker credential helper)
	manager.mu.Lock()
	defer manager.mu.Unlock()

	originalStack, processedStack := manager.stacks[edgeStackID(stackPayload.ID)]
	if processedStack {
		// update the cloned stack to keep data consistency
		clonedStack := *originalStack
		stack = &clonedStack

		if deleteStack {
			log.Debug().Int("stack_id", stackPayload.ID).Msg("marking stack for removal")

			stack.Action = actionDelete
		} else {
			if stack.Version == stackPayload.Version && !stackPayload.ReadyRePullImage {
				return nil
			}

			log.Debug().Int("stack_id", stackPayload.ID).Msg("marking stack for update")

			stack.Action = actionUpdate
			stack.ReadyRePullImage = stackPayload.ReadyRePullImage
		}
	} else {
		if deleteStack {
			log.Debug().Int("stack_id", stackPayload.ID).Msg("marking stack for removal")

			stack = &edgeStack{
				StackPayload: edge.StackPayload{
					ID: stackPayload.ID,
				},
				Action: actionDelete,
			}
		} else {
			log.Debug().Int("stack_id", stackPayload.ID).Msg("marking stack for deployment")

			stack = &edgeStack{
				StackPayload: edge.StackPayload{
					ID: stackPayload.ID,
				},
				Action: actionDeploy,
			}
		}
	}

	edgeIdPair := portainer.Pair{Name: agent.EdgeIdEnvVarName, Value: manager.edgeID}

	stack.Name = stackPayload.Name
	stack.RegistryCredentials = stackPayload.RegistryCredentials

	stack.Status = StatusPending
	stack.Version = stackPayload.Version

	stack.PrePullImage = stackPayload.PrePullImage
	stack.RePullImage = stackPayload.RePullImage
	stack.RetryDeploy = stackPayload.RetryDeploy
	stack.RetryPeriod = stackPayload.RetryPeriod
	stack.PullCount = 0
	stack.PullFinished = false
	stack.DeployCount = 0
	stack.DeployerOptionsPayload = stackPayload.DeployerOptionsPayload

	stack.SupportRelativePath = stackPayload.SupportRelativePath
	stack.FilesystemPath = stackPayload.FilesystemPath
	stack.FileName = stackPayload.EntryFileName
	stack.FileFolder = getStackFileFolder(stack)
	stack.EnvVars = append(stackPayload.EnvVars, edgeIdPair)
	stack.Namespace = stackPayload.Namespace

	if err := filesystem.DecodeDirEntries(stackPayload.DirEntries); err != nil {
		return err
	}

	if err := manager.addRegistryToEntryFile(&stackPayload); err != nil {
		return err
	}

	if !deleteStack {
		if err := filesystem.PersistDir(stack.FileFolder, stackPayload.DirEntries); err != nil {
			return err
		}
	}

	manager.stacks[edgeStackID(stack.ID)] = stack

	return nil
}
