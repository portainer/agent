package stack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/yaml"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/nomad"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/libstack"

	"github.com/rs/zerolog/log"
)

type edgeStackID int

type edgeStack struct {
	edge.StackPayload

	FileFolder string
	FileName   string
	Status     edgeStackStatus
	Action     edgeStackAction

	PullCount    int
	PullFinished bool
	DeployCount  int
}

type edgeStackStatus int

const (
	_ edgeStackStatus = iota
	StatusPending
	StatusDeployed
	StatusError
	StatusDeploying
	StatusRetry
	StatusRemoving
	StatusAwaitingDeployedStatus
	StatusAwaitingRemovedStatus
)

type edgeStackAction int

const (
	_ edgeStackAction = iota
	actionDeploy
	actionUpdate
	actionDelete
	actionIdle
)

const queueSleepInterval = agent.EdgeStackQueueSleepIntervalSeconds * time.Second
const perHourRetries = 3600 / 5
const maxRetries = perHourRetries * 24 * 7 // retry for maximum 1 week

type engineType int

const (
	// TODO: consider defining this in agent.go or re-use/enhance some of the existing constants
	// that are declared in agent.go
	_ engineType = iota
	EngineTypeDockerStandalone
	EngineTypeDockerSwarm
	EngineTypeKubernetes
	EngineTypeNomad
)

// StackManager represents a service for managing Edge stacks
type StackManager struct {
	engineType      engineType
	edgeID          string
	stacks          map[edgeStackID]*edgeStack
	stopSignal      chan struct{}
	deployer        agent.Deployer
	isEnabled       bool
	portainerClient client.PortainerClient
	assetsPath      string
	awsConfig       *agent.AWSConfig
	mu              sync.Mutex
}

// NewStackManager returns a pointer to a new instance of StackManager
func NewStackManager(cli client.PortainerClient, assetsPath string, config *agent.AWSConfig, edgeID string) *StackManager {
	return &StackManager{
		stacks:          map[edgeStackID]*edgeStack{},
		stopSignal:      nil,
		portainerClient: cli,
		assetsPath:      assetsPath,
		awsConfig:       config,
		edgeID:          edgeID,
	}
}

func (manager *StackManager) UpdateStacksStatus(pollResponseStacks map[int]client.StackStatus) error {
	if !manager.isEnabled {
		return nil
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	for stackID, status := range pollResponseStacks {
		err := manager.processStack(stackID, status)
		if err != nil {
			return err
		}
	}

	manager.processRemovedStacks(pollResponseStacks)

	return nil
}

func (manager *StackManager) addRegistryToEntryFile(stackPayload *edge.StackPayload) error {
	var fileContent *string

	for index, dirEntry := range stackPayload.DirEntries {
		if dirEntry.IsFile && dirEntry.Name == stackPayload.EntryFileName {
			fileContent = &stackPayload.DirEntries[index].Content
			break
		}
	}

	if fileContent == nil {
		return fmt.Errorf("EntryFileName not found in DirEntries")
	}

	switch manager.engineType {
	case EngineTypeDockerStandalone, EngineTypeDockerSwarm:
		if (len(stackPayload.RegistryCredentials) > 0 || manager.awsConfig != nil) && stackPayload.EdgeUpdateID > 0 {
			var err error
			yml := yaml.NewDockerComposeYAML(*fileContent, stackPayload.RegistryCredentials, manager.awsConfig)
			*fileContent, err = yml.AddCredentialsAsEnvForSpecificService("updater")
			if err != nil {
				return err
			}
		}

	case EngineTypeKubernetes:
		if len(stackPayload.RegistryCredentials) > 0 {
			yml := yaml.NewKubernetesYAML(*fileContent, stackPayload.RegistryCredentials)
			*fileContent, _ = yml.AddImagePullSecrets()
		}
	}

	return nil
}

func getStackFileFolder(stack *edgeStack) string {
	stackIDStr := strconv.Itoa(stack.ID)

	folder := filepath.Join(agent.EdgeStackFilesPath, stackIDStr)
	if IsRelativePathStack(stack) {
		folder = filepath.Join(stack.FilesystemPath, agent.ComposePathPrefix, stackIDStr)
	}

	return folder
}

func (manager *StackManager) processStack(stackID int, stackStatus client.StackStatus) error {
	var stack *edgeStack

	originalStack, processedStack := manager.stacks[edgeStackID(stackID)]
	if processedStack {
		// update the cloned stack to keep data consistency
		clonedStack := *originalStack
		stack = &clonedStack

		if stack.Version == stackStatus.Version && !stackStatus.ReadyRePullImage {
			return nil // stack is unchanged
		}

		log.Debug().Int("stack_identifier", stackID).Msg("marking stack for update")

		stack.Action = actionUpdate
		stack.Version = stackStatus.Version
		stack.Status = StatusPending

		stack.PullFinished = false
		stack.PullCount = 0
		stack.DeployCount = 0
	} else {
		log.Debug().Int("stack_identifier", stackID).Msg("marking stack for deployment")

		stack = &edgeStack{
			StackPayload: edge.StackPayload{
				Version: stackStatus.Version,
				ID:      stackID,
			},
			Action: actionDeploy,
			Status: StatusPending,
		}
	}

	stackPayload, err := manager.portainerClient.GetEdgeStackConfig(stackID, &stackStatus.Version)
	if err != nil {
		return err
	}

	edgeIdPair := portainer.Pair{Name: agent.EdgeIdEnvVarName, Value: manager.edgeID}

	stack.Name = stackPayload.Name
	stack.RegistryCredentials = stackPayload.RegistryCredentials
	stack.Namespace = stackPayload.Namespace
	stack.PrePullImage = stackPayload.PrePullImage
	stack.RePullImage = stackPayload.RePullImage
	stack.RetryDeploy = stackPayload.RetryDeploy
	stack.EnvVars = append(stackPayload.EnvVars, edgeIdPair)
	stack.SupportRelativePath = stackPayload.SupportRelativePath
	stack.FilesystemPath = stackPayload.FilesystemPath
	stack.FileName = stackPayload.EntryFileName
	stack.FileFolder = getStackFileFolder(stack)
	stack.RollbackTo = stackPayload.RollbackTo

	err = filesystem.DecodeDirEntries(stackPayload.DirEntries)
	if err != nil {
		return err
	}

	err = manager.addRegistryToEntryFile(stackPayload)
	if err != nil {
		return err
	}

	err = filesystem.PersistDir(stack.FileFolder, stackPayload.DirEntries)
	if err != nil {
		return err
	}

	manager.stacks[edgeStackID(stackID)] = stack

	log.Debug().
		Int("stack_identifier", int(stack.ID)).
		Str("stack_name", stack.Name).
		Str("namespace", stack.Namespace).
		Msg("stack acknowledged")

	return manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusAcknowledged, stack.RollbackTo, "")
}

func (manager *StackManager) processRemovedStacks(pollResponseStacks map[int]client.StackStatus) {
	for stackID, stack := range manager.stacks {
		if _, ok := pollResponseStacks[int(stackID)]; !ok {
			log.Debug().Int("stack_identifier", int(stackID)).Msg("marking stack for deletion")

			stack.Action = actionDelete
			stack.Status = StatusPending

			manager.stacks[stackID] = stack
		}
	}
}

func (manager *StackManager) Stop() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.stopSignal != nil {
		close(manager.stopSignal)
		manager.stopSignal = nil
		manager.isEnabled = false
	}

	return nil
}

func (manager *StackManager) Start() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.stopSignal != nil {
		return nil
	}

	manager.isEnabled = true
	manager.stopSignal = make(chan struct{})

	go func() {
		for {
			manager.mu.Lock()

			select {
			case <-manager.stopSignal:
				manager.mu.Unlock()

				log.Debug().Msg("shutting down Edge stack manager")
				return
			default:
				manager.mu.Unlock()

				manager.performActionOnStack()
			}
		}
	}()

	return nil
}

func (manager *StackManager) performActionOnStack() {
	stack := manager.nextPendingStack()
	if stack == nil {
		time.Sleep(queueSleepInterval)

		return
	}

	ctx := context.TODO()
	manager.mu.Lock()
	stackName := fmt.Sprintf("edge_%s", stack.Name)
	stackFileLocation := fmt.Sprintf("%s/%s", stack.FileFolder, stack.FileName)
	manager.mu.Unlock()

	if stack.Status == StatusAwaitingDeployedStatus || stack.Status == StatusAwaitingRemovedStatus {
		err := manager.checkStackStatus(ctx, stackName, stack)
		if err != nil {
			log.Error().Err(err).Msg("unable to check Edge stack status")
		}

		return
	}

	switch stack.Action {
	case actionDeploy, actionUpdate:
		// validate the stack file and fail-fast if the stack format is invalid
		// each deployer has its own Validate function
		err := manager.validateStackFile(ctx, stack, stackName, stackFileLocation)
		if err != nil {
			return
		}

		err = manager.pullImages(ctx, stack, stackName, stackFileLocation)
		if err != nil {
			return
		}

		if IsRelativePathStack(stack) {
			dst := filepath.Join(stack.FilesystemPath, agent.ComposePathPrefix)
			err := docker.CopyGitStackToHost(stack.FileFolder, dst, stack.ID, stackName, manager.assetsPath)
			if err != nil {
				return
			}
		}

		manager.deployStack(ctx, stack, stackName, stackFileLocation)
	case actionDelete:
		stackFileLocation = fmt.Sprintf("%s/%s", SuccessStackFileFolder(stack.FileFolder), stack.FileName)
		manager.deleteStack(ctx, stack, stackName, stackFileLocation)
		if IsRelativePathStack(stack) {
			dst := filepath.Join(stack.FilesystemPath, agent.ComposePathPrefix)
			_ = docker.RemoveGitStackFromHost(stack.FileFolder, dst, stack.ID, stackName)
		}
	}
}

func (manager *StackManager) nextPendingStack() *edgeStack {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	// find the first pending stack,
	// if not found look for a stack waiting for status check
	// if not found, look for the first retry stack and set it to pending

	for _, stack := range manager.stacks {
		if stack.Status == StatusPending {
			return stack
		}
	}

	for _, stack := range manager.stacks {
		if stack.Status == StatusAwaitingDeployedStatus || stack.Status == StatusAwaitingRemovedStatus {
			return stack
		}
	}

	for _, stack := range manager.stacks {
		if stack.Status == StatusRetry {
			log.Debug().
				Int("stack_identifier", int(stack.ID)).
				Msg("retrying stack")

			stack.Status = StatusPending
		}
	}

	return nil
}

func (manager *StackManager) checkStackStatus(ctx context.Context, stackName string, stack *edgeStack) error {
	log.Debug().
		Int("stack_identifier", int(stack.ID)).
		Str("stack_name", stackName).
		Msg("checking stack status")

	manager.mu.Lock()
	defer manager.mu.Unlock()

	requiredStatus := libstack.StatusRemoved
	if stack.Status == StatusAwaitingDeployedStatus {
		requiredStatus = libstack.StatusRunning
	}

	status, statusMessage, err := manager.waitForStatus(ctx, stackName, requiredStatus)
	if err != nil {
		return err
	}

	log.Debug().
		Int("stack_identifier", int(stack.ID)).
		Str("stack_name", stackName).
		Str("status", string(status)).
		Str("status_message", statusMessage).
		Int("old_status", int(stack.Status)).
		Msg("stack status")

	if status == libstack.StatusError {
		stack.Status = StatusError
		return manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusError, stack.RollbackTo, statusMessage)
	}

	if status == libstack.StatusRunning {
		stack.Status = StatusDeployed
		return manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusRunning, stack.RollbackTo, "")
	}

	if status == libstack.StatusRemoved {
		delete(manager.stacks, edgeStackID(stack.ID))
		return manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusRemoved, stack.RollbackTo, "")
	}

	return nil
}

func (manager *StackManager) waitForStatus(ctx context.Context, stackName string, requiredStatus libstack.Status) (libstack.Status, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	statusCh := manager.deployer.WaitForStatus(ctx, stackName, requiredStatus)
	result := <-statusCh
	if result == "" {
		return requiredStatus, "", nil
	}

	return libstack.StatusError, result, nil

}

func (manager *StackManager) validateStackFile(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	log.Debug().Int("stack_identifier", int(stack.ID)).
		Str("stack_name", stackName).
		Str("namespace", stack.Namespace).
		Msg("validating stack")

	envVars := buildEnvVarsForDeployer(stack.EnvVars)

	err := manager.deployer.Validate(ctx, stackName, []string{stackFileLocation},
		agent.ValidateOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        envVars,
			},
		},
	)
	if err != nil {
		log.Error().Int("stack_identifier", int(stack.ID)).Err(err).Msg("stack validation failed")
		stack.Status = StatusError

		statusUpdateErr := manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusError, stack.RollbackTo, err.Error())
		if statusUpdateErr != nil {
			log.Error().Err(statusUpdateErr).Msg("unable to update Edge stack status")
		}
	} else {
		log.Debug().Int("stack_identifier", int(stack.ID)).Int("stack_version", stack.Version).Msg("stack validated")
	}

	return err
}

func (manager *StackManager) pullImages(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if stack.PullFinished || (!stack.PrePullImage && !stack.RePullImage) {
		return nil
	}

	log.Debug().Int("stack_identifier", int(stack.ID)).Msg("pulling images")

	stack.PullCount += 1
	if stack.PullCount > perHourRetries && stack.PullCount%perHourRetries != 0 {
		return fmt.Errorf("skip pulling")
	}

	stack.Status = StatusDeploying

	envVars := buildEnvVarsForDeployer(stack.EnvVars)

	err := manager.deployer.Pull(ctx, stackName, []string{stackFileLocation}, agent.PullOptions{
		DeployerBaseOptions: agent.DeployerBaseOptions{
			WorkingDir: stack.FileFolder,
			Env:        envVars,
		},
	})
	if err != nil {
		log.Error().Err(err).
			Int("stack_identifier", int(stack.ID)).
			Int("PullCount", stack.PullCount).
			Msg("images pull failed")

		if stack.PullCount < maxRetries {
			stack.Status = StatusRetry

			return err
		}

		stack.Status = StatusError

		statusUpdateErr := manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusError, stack.RollbackTo, err.Error())
		if statusUpdateErr != nil {
			log.Error().
				Err(statusUpdateErr).
				Int("stack_identifier", int(stack.ID)).
				Msg("unable to update Edge stack status")
		}

		return err
	}

	stack.PullFinished = true

	log.Debug().
		Int("stack_identifier", int(stack.ID)).
		Int("stack_version", stack.Version).
		Msg("images pulled")

	statusUpdateErr := manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusImagesPulled, stack.RollbackTo, "")
	if statusUpdateErr != nil {
		log.Error().
			Err(statusUpdateErr).
			Int("stack_identifier", int(stack.ID)).
			Msg("unable to update Edge stack status")
	}

	return err
}

func (manager *StackManager) deployStack(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	stack.DeployCount += 1

	err := manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusDeploying, stack.RollbackTo, "")
	if err != nil {
		log.Error().Err(err).Msg("unable to update Edge stack status")
	}

	stack.Status = StatusDeploying

	log.Debug().
		Int("stack_identifier", int(stack.ID)).
		Bool("RetryDeploy", stack.RetryDeploy).
		Int("DeployCount", stack.DeployCount).
		Str("stack_name", stackName).
		Str("namespace", stack.Namespace).
		Msg("stack deployment")

	if stack.DeployCount > perHourRetries && stack.DeployCount%perHourRetries != 0 {
		return
	}

	envVars := buildEnvVarsForDeployer(stack.EnvVars)

	err = manager.deployer.Deploy(ctx, stackName, []string{stackFileLocation},
		agent.DeployOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: stack.FileFolder,
				Env:        envVars,
			},
		},
	)

	if err != nil {
		log.Error().Err(err).Int("DeployCount", stack.DeployCount).Msg("stack deployment failed")

		if stack.RetryDeploy && stack.DeployCount < maxRetries {
			stack.Status = StatusRetry
			return
		}

		stack.Status = StatusError

		err = manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusError, stack.RollbackTo, "failed to redeploy stack")
		if err != nil {
			log.Error().Err(err).Msg("unable to update Edge stack status")
		}

		return
	}

	stack.Action = actionIdle

	log.Debug().
		Int("stack_identifier", int(stack.ID)).
		Int("stack_version", stack.Version).Msg("stack deployed")

	err = manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusDeploymentReceived, stack.RollbackTo, "")
	if err != nil {
		log.Error().Err(err).Msg("unable to update Edge stack status")
	}

	err = backupSuccessStack(stack)
	if err != nil {
		log.Error().Err(err).Msg("unable to backup successful Edge stack")
	}

	stack.Status = StatusAwaitingDeployedStatus

}

func buildEnvVarsForDeployer(envVars []portainer.Pair) []string {
	arr := make([]string, len(envVars))
	for i, env := range envVars {
		arr[i] = fmt.Sprintf("%s=%s", env.Name, env.Value)
	}
	return arr
}

func (manager *StackManager) deleteStack(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	stack.Status = StatusRemoving
	log.Debug().Int("stack_identifier", int(stack.ID)).Msg("removing stack")

	successFileFolder := SuccessStackFileFolder(stack.FileFolder)
	err := manager.deployer.Remove(
		ctx,
		stackName,
		[]string{stackFileLocation},
		agent.RemoveOptions{
			DeployerBaseOptions: agent.DeployerBaseOptions{
				Namespace:  stack.Namespace,
				WorkingDir: successFileFolder,
				Env:        buildEnvVarsForDeployer(stack.EnvVars),
			},
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("unable to remove stack")

		return
	}

	err = manager.portainerClient.SetEdgeStackStatus(int(stack.ID), portainer.EdgeStackStatusRemoving, stack.RollbackTo, "")
	if err != nil {
		log.Error().Err(err).Msg("unable to delete Edge stack status")

		return
	}

	stack.Status = StatusAwaitingRemovedStatus

	// Remove stack file folder
	err = os.RemoveAll(stack.FileFolder)
	if err != nil {
		log.Error().Err(err).Msgf("unable to delete Edge stack folder %s", stack.FileFolder)
	}

	// Remove success stack file folder
	err = os.RemoveAll(successFileFolder)
	if err != nil {
		log.Error().Err(err).Msgf("unable to delete Edge stack folder %s", successFileFolder)
	}
}

func (manager *StackManager) SetEngineStatus(engineStatus engineType) error {
	if engineStatus == manager.engineType {
		return nil
	}

	manager.engineType = engineStatus

	err := manager.Stop()
	if err != nil {
		return err
	}

	deployer, err := buildDeployerService(manager.assetsPath, engineStatus)
	if err != nil {
		return err
	}
	manager.deployer = deployer

	return nil
}

func buildDeployerService(assetsPath string, engineStatus engineType) (agent.Deployer, error) {
	switch engineStatus {
	case EngineTypeDockerStandalone:
		return exec.NewDockerComposeStackService(assetsPath)
	case EngineTypeDockerSwarm:
		return exec.NewDockerSwarmStackService(assetsPath)
	case EngineTypeKubernetes:
		return exec.NewKubernetesDeployer(assetsPath), nil
	case EngineTypeNomad:
		return nomad.NewDeployer()
	}

	return nil, fmt.Errorf("engine status %d not supported", engineStatus)
}

func (manager *StackManager) DeployStack(ctx context.Context, stackData edge.StackPayload) error {
	return manager.buildDeployerParams(stackData, false)
}

func (manager *StackManager) DeleteStack(ctx context.Context, stackData edge.StackPayload) error {
	return manager.buildDeployerParams(stackData, true)
}

func (manager *StackManager) buildDeployerParams(stackPayload edge.StackPayload, deleteStack bool) error {
	var err error
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

	stack.Name = stackPayload.Name
	stack.RegistryCredentials = stackPayload.RegistryCredentials

	stack.Status = StatusPending
	stack.Version = stackPayload.Version

	stack.PrePullImage = stackPayload.PrePullImage
	stack.RePullImage = stackPayload.RePullImage
	stack.RetryDeploy = stackPayload.RetryDeploy
	stack.PullCount = 0
	stack.PullFinished = false
	stack.DeployCount = 0

	stack.SupportRelativePath = stackPayload.SupportRelativePath
	stack.FilesystemPath = stackPayload.FilesystemPath
	stack.FileName = stackPayload.EntryFileName
	stack.FileFolder = getStackFileFolder(stack)
	stack.EnvVars = stackPayload.EnvVars

	err = filesystem.DecodeDirEntries(stackPayload.DirEntries)
	if err != nil {
		return err
	}

	err = manager.addRegistryToEntryFile(&stackPayload)
	if err != nil {
		return err
	}

	if !deleteStack {
		err = filesystem.PersistDir(stack.FileFolder, stackPayload.DirEntries)
		if err != nil {
			return err
		}
	}

	manager.stacks[edgeStackID(stack.ID)] = stack

	return nil
}

func (manager *StackManager) GetEdgeRegistryCredentials() []edge.RegistryCredentials {
	for _, stack := range manager.stacks {
		if stack.Status == StatusDeploying {
			return stack.RegistryCredentials
		}
	}

	return nil
}

func (manager *StackManager) DeleteNormalStack(ctx context.Context, stackName string) error {
	log.Debug().Str("stack_name", stackName).Msg("removing normal stack")

	err := manager.deployer.Remove(ctx, stackName, []string{}, agent.RemoveOptions{})
	if err != nil {
		log.Error().Err(err).Msg("unable to remove normal stack")
		return err
	}

	return nil
}
