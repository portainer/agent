package stack

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/filesystem"
)

type edgeStackID int

type edgeStack struct {
	ID         edgeStackID
	Name       string
	Version    int
	FileFolder string
	FileName   string
	Status     edgeStackStatus
	Action     edgeStackAction
}

type edgeStackStatus int

const (
	_ edgeStackStatus = iota
	statusPending
	statusDone
	statusError
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
	edgeStackStatusOk
	edgeStackStatusError
	edgeStackStatusAcknowledged
)

type engineType int

const (
	_ engineType = iota
	EngineTypeDockerStandalone
	EngineTypeDockerSwarm
	EngineTypeKubernetes
)

// StackManager represents a service for managing Edge stacks
type StackManager struct {
	engineType engineType
	stacks     map[edgeStackID]*edgeStack
	stopSignal chan struct{}
	deployer   agent.Deployer
	isEnabled  bool
	httpClient *client.PortainerClient
}

// NewStackManager returns a pointer to a new instance of StackManager
func NewStackManager(portainerURL, endpointID, edgeID string, insecurePoll bool) (*StackManager, error) {
	cli := client.NewPortainerClient(portainerURL, endpointID, edgeID, insecurePoll)

	stackManager := &StackManager{
		stacks:     map[edgeStackID]*edgeStack{},
		stopSignal: nil,
		httpClient: cli,
	}

	return stackManager, nil
}

func (manager *StackManager) UpdateStacksStatus(stacks map[int]int) error {
	if !manager.isEnabled {
		return nil
	}

	for stackID, version := range stacks {
		stack, ok := manager.stacks[edgeStackID(stackID)]
		if ok {
			if stack.Version == version {
				continue
			}
			log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: marking stack for update]", stackID)

			stack.Action = actionUpdate
			stack.Version = version
			stack.Status = statusPending
		} else {
			log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: marking stack for deployment]", stackID)

			stack = &edgeStack{
				Action:  actionDeploy,
				ID:      edgeStackID(stackID),
				Status:  statusPending,
				Version: version,
			}
		}

		stackConfig, err := manager.httpClient.GetEdgeStackConfig(int(stack.ID))
		if err != nil {
			return err
		}

		stack.Name = stackConfig.Name

		folder := fmt.Sprintf("%s/%d", agent.EdgeStackFilesPath, stackID)
		fileName := "docker-compose.yml"
		if manager.engineType == EngineTypeKubernetes {
			fileName = fmt.Sprintf("%s.yml", stack.Name)
		}

		err = filesystem.WriteFile(folder, fileName, []byte(stackConfig.FileContent), 0644)
		if err != nil {
			return err
		}

		stack.FileFolder = folder
		stack.FileName = fileName

		manager.stacks[stack.ID] = stack

		err = manager.httpClient.SetEdgeStackStatus(int(stack.ID), int(edgeStackStatusAcknowledged), "")
		if err != nil {
			return err
		}
	}

	for stackID, stack := range manager.stacks {
		if _, ok := stacks[int(stackID)]; !ok {
			log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: marking stack for deletion]", stackID)
			stack.Action = actionDelete
			stack.Status = statusPending

			manager.stacks[stackID] = stack
		}
	}

	return nil
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
				log.Println("[DEBUG] [internal,edge,stack] [message: shutting down Edge stack manager]")
				return
			default:
				stack := manager.next()
				if stack == nil {
					timer1 := time.NewTimer(queueSleepInterval)
					<-timer1.C
					continue
				}

				stackName := fmt.Sprintf("edge_%s", stack.Name)
				stackFileLocation := fmt.Sprintf("%s/%s", stack.FileFolder, stack.FileName)

				ctx := context.TODO()

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

func (manager *StackManager) next() *edgeStack {
	for _, stack := range manager.stacks {
		if stack.Status == statusPending {
			return stack
		}
	}
	return nil
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

	deployer, err := buildDeployerService(engineStatus)
	if err != nil {
		return err
	}
	manager.deployer = deployer

	return nil
}

func (manager *StackManager) deployStack(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) {
	log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: stack deployment]", stack.ID)
	stack.Status = statusDone
	stack.Action = actionIdle
	responseStatus := int(edgeStackStatusOk)
	errorMessage := ""

	err := manager.deployer.Deploy(ctx, stackName, []string{stackFileLocation}, false)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: stack deployment failed] [error: %s]", err)
		stack.Status = statusError
		responseStatus = int(edgeStackStatusError)
		errorMessage = err.Error()
	} else {
		log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [stack_version: %d] [message: stack deployed]", stack.ID, stack.Version)
	}

	manager.stacks[stack.ID] = stack

	err = manager.httpClient.SetEdgeStackStatus(int(stack.ID), responseStatus, errorMessage)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to update Edge stack status] [error: %s]", err)
	}
}

func (manager *StackManager) deleteStack(ctx context.Context, stack *edgeStack, stackName, stackFileLocation string) {
	log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: removing stack]", stack.ID)
	err := manager.deployer.Remove(ctx, stackName, []string{stackFileLocation})
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to remove stack] [error: %s]", err)
		return
	}

	err = filesystem.RemoveFile(stackFileLocation)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to delete Edge stack file] [error: %s]", err)
		return
	}

	delete(manager.stacks, stack.ID)
}

func buildDeployerService(engineStatus engineType) (agent.Deployer, error) {
	switch engineStatus {
	case EngineTypeDockerStandalone:
		return exec.NewDockerComposeStackService(agent.DockerBinaryPath)
	case EngineTypeDockerSwarm:
		return exec.NewDockerSwarmStackService(agent.DockerBinaryPath)
	case EngineTypeKubernetes:
		return exec.NewKubernetesDeployer(agent.DockerBinaryPath), nil
	}

	return nil, fmt.Errorf("engine status %d not supported", engineStatus)
}
