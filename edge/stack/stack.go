package stack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/yaml"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/nomad"

	"github.com/rs/zerolog/log"
)

type edgeStackID int

type edgeStack struct {
	ID                  edgeStackID
	Name                string
	Version             int
	FileFolder          string
	FileName            string
	Status              edgeStackStatus
	Action              edgeStackAction
	RegistryCredentials []agent.RegistryCredentials
}

type edgeStackStatus int

const (
	_ edgeStackStatus = iota
	StatusPending
	StatusDone
	StatusError
	StatusDeploying
)

type edgeStackAction int

const (
	_ edgeStackAction = iota
	actionDeploy
	actionUpdate
	actionDelete
	actionIdle
)

type edgeStackStatusType int

const (
	_ edgeStackStatusType = iota
	EdgeStackStatusOk
	EdgeStackStatusError
	EdgeStackStatusAcknowledged
	EdgeStackStatusRemove
)

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
	stacks          map[edgeStackID]*edgeStack
	stopSignal      chan struct{}
	deployer        agent.Deployer
	isEnabled       bool
	portainerClient client.PortainerClient
	assetsPath      string
	mu              sync.Mutex
}

// NewStackManager returns a pointer to a new instance of StackManager
func NewStackManager(cli client.PortainerClient, assetsPath string) *StackManager {
	return &StackManager{
		stacks:          map[edgeStackID]*edgeStack{},
		stopSignal:      nil,
		portainerClient: cli,
		assetsPath:      assetsPath,
	}
}

func (manager *StackManager) UpdateStacksStatus(pollResponseStacks map[int]int) error {
	if !manager.isEnabled {
		return nil
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	for stackID, version := range pollResponseStacks {
		err := manager.processStack(stackID, version)
		if err != nil {
			return err
		}
	}

	manager.processRemovedStacks(pollResponseStacks)

	return nil
}

func (manager *StackManager) processStack(stackID int, version int) error {
	stack, processedStack := manager.stacks[edgeStackID(stackID)]
	if processedStack {
		if stack.Version == version {
			return nil // stack is unchanged
		}

		log.Debug().Int("stack_identifier", stackID).Msg("marking stack for update")

		stack.Action = actionUpdate
		stack.Version = version
		stack.Status = StatusPending
	} else {
		log.Debug().Int("stack_identifier", stackID).Msg("marking stack for deployment")

		stack = &edgeStack{
			ID:      edgeStackID(stackID),
			Action:  actionDeploy,
			Status:  StatusPending,
			Version: version,
		}
	}

	stackConfig, err := manager.portainerClient.GetEdgeStackConfig(int(stack.ID))
	if err != nil {
		return err
	}

	stack.Name = stackConfig.Name
	stack.RegistryCredentials = stackConfig.RegistryCredentials

	folder := fmt.Sprintf("%s/%d", agent.EdgeStackFilesPath, stackID)
	fileName := "docker-compose.yml"
	fileContent := stackConfig.FileContent
	if manager.engineType == EngineTypeKubernetes {
		fileName = fmt.Sprintf("%s.yml", stack.Name)
		if len(stackConfig.RegistryCredentials) > 0 {
			yml := yaml.NewYAML(fileContent, stackConfig.RegistryCredentials)
			fileContent, _ = yml.AddImagePullSecrets()
		}
	}
	if manager.engineType == EngineTypeNomad {
		fileName = fmt.Sprintf("%s.hcl", stack.Name)
	}

	err = filesystem.WriteFile(folder, fileName, []byte(fileContent), 0644)
	if err != nil {
		return err
	}

	stack.FileFolder = folder
	stack.FileName = fileName

	manager.stacks[stack.ID] = stack

	return manager.portainerClient.SetEdgeStackStatus(int(stack.ID), int(EdgeStackStatusAcknowledged), "")
}

func (manager *StackManager) processRemovedStacks(pollResponseStacks map[int]int) {
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
	if manager.stopSignal != nil {
		close(manager.stopSignal)
		manager.stopSignal = nil
		manager.isEnabled = false
	}

	return nil
}

func (manager *StackManager) Start() error {
	if manager.stopSignal != nil {
		return nil
	}

	manager.isEnabled = true
	manager.stopSignal = make(chan struct{})

	queueSleepInterval, err := time.ParseDuration(agent.EdgeStackQueueSleepInterval)
	if err != nil {
		return err
	}

	go (func() {
		for {
			select {
			case <-manager.stopSignal:
				log.Debug().Msg("shutting down Edge stack manager")

				return
			default:
				stack := manager.nextPendingStack()
				if stack == nil {
					timer1 := time.NewTimer(queueSleepInterval)
					<-timer1.C
					continue
				}

				ctx := context.TODO()

				manager.mu.Lock()
				stackName := fmt.Sprintf("edge_%s", stack.Name)
				stackFileLocation := fmt.Sprintf("%s/%s", stack.FileFolder, stack.FileName)
				manager.mu.Unlock()

				if stack.Action == actionDeploy || stack.Action == actionUpdate {
					manager.deployStack(ctx, stack, stackName, stackFileLocation)
				} else if stack.Action == actionDelete {
					manager.deleteStack(ctx, stack, stackName, stackFileLocation)
				}
			}
		}
	})()

	return nil
}

func (manager *StackManager) nextPendingStack() *edgeStack {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	for _, stack := range manager.stacks {
		if stack.Status == StatusPending {
			return stack
		}
	}
	return nil
}

func (manager *StackManager) deployStack(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	log.Debug().Int("stack_identifier", int(stack.ID)).Msg("stack deployment")

	stack.Status = StatusDeploying
	stack.Action = actionIdle
	responseStatus := int(EdgeStackStatusOk)
	errorMessage := ""

	err := manager.deployer.Deploy(ctx, stackName, []string{stackFileLocation}, false)
	if err != nil {
		log.Error().Err(err).Msg("stack deployment failed")

		stack.Status = StatusError
		responseStatus = int(EdgeStackStatusError)
		errorMessage = err.Error()
	} else {
		log.Debug().Int("stack_identifier", int(stack.ID)).Int("stack_version", stack.Version).Msg("stack deployed")

		stack.Status = StatusDone
	}

	manager.stacks[stack.ID] = stack

	err = manager.portainerClient.SetEdgeStackStatus(int(stack.ID), responseStatus, errorMessage)
	if err != nil {
		log.Error().Err(err).Msg("unable to update Edge stack status")
	}
}

func (manager *StackManager) deleteStack(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) {
	log.Debug().Int("stack_identifier", int(stack.ID)).Msg("removing stack")

	err := manager.deployer.Remove(ctx, stackName, []string{stackFileLocation})
	if err != nil {
		log.Error().Err(err).Msg("unable to remove stack")

		return
	}

	// Remove stack file folder
	err = os.RemoveAll(filepath.Dir(stackFileLocation))
	if err != nil {
		log.Error().Err(err).Msg("unable to delete Edge stack file")

		return
	}

	err = manager.portainerClient.DeleteEdgeStackStatus(int(stack.ID))
	if err != nil {
		log.Error().Err(err).Msg("unable to delete Edge stack status")

		return
	}

	manager.mu.Lock()
	delete(manager.stacks, stack.ID)
	manager.mu.Unlock()
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

func (manager *StackManager) DeployStack(ctx context.Context, stackData client.EdgeStackData) error {
	stackName, stackFileLocation, err := manager.buildDeployerParams(stackData, true)
	if err != nil {
		return err
	}

	return manager.deployer.Deploy(ctx, stackName, []string{stackFileLocation}, false)
}

func (manager *StackManager) DeleteStack(ctx context.Context, stackData client.EdgeStackData) error {
	stackName, stackFileLocation, err := manager.buildDeployerParams(stackData, false)
	if err != nil {
		return err
	}
	return manager.deployer.Remove(ctx, stackName, []string{stackFileLocation})
}

func (manager *StackManager) buildDeployerParams(stackData client.EdgeStackData, writeFile bool) (string, string, error) {
	folder := fmt.Sprintf("%s/%d", agent.EdgeStackFilesPath, stackData.ID)
	fileName := "docker-compose.yml"
	fileContent := stackData.StackFileContent
	if manager.engineType == EngineTypeKubernetes {
		fileName = fmt.Sprintf("%s.yml", stackData.Name)
		if len(stackData.RegistryCredentials) > 0 {
			yml := yaml.NewYAML(fileContent, stackData.RegistryCredentials)
			fileContent, _ = yml.AddImagePullSecrets()
		}
	}

	if manager.engineType == EngineTypeKubernetes {
		fileName = fmt.Sprintf("%s.yml", stackData.Name)
	}

	if writeFile {
		err := filesystem.WriteFile(folder, fileName, []byte(fileContent), 0644)
		if err != nil {
			return "", "", err
		}
	}

	stackName := fmt.Sprintf("edge_%s", stackData.Name)
	stackFileLocation := fmt.Sprintf("%s/%s", folder, fileName)
	return stackName, stackFileLocation, nil
}

func (manager *StackManager) GetEdgeRegistryCredentials() []agent.RegistryCredentials {
	for _, stack := range manager.stacks {
		if stack.Status == StatusDeploying {
			return stack.RegistryCredentials
		}
	}

	return nil
}
