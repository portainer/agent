package edge

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/client"
	"github.com/portainer/agent/nomad"
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
	engineTypeDockerStandalone
	engineTypeDockerSwarm
	engineTypeKubernetes
	engineTypeNomad
)

// StackManager represents a service for managing Edge stacks
type StackManager struct {
	engineType   engineType
	stacks       map[edgeStackID]*edgeStack
	stopSignal   chan struct{}
	deployer     agent.Deployer
	portainerURL string
	endpointID   string
	isEnabled    bool
	httpClient   *client.PortainerClient
	mu           sync.Mutex
}

// newStackManager returns a pointer to a new instance of StackManager
func newStackManager(portainerURL, endpointID, edgeID string, insecurePoll bool, tunnel bool) (*StackManager, error) {
	cli := client.NewPortainerClient(portainerURL, endpointID, edgeID, insecurePoll, tunnel)

	stackManager := &StackManager{
		stacks:     map[edgeStackID]*edgeStack{},
		stopSignal: nil,
		httpClient: cli,
	}

	return stackManager, nil
}

func (manager *StackManager) updateStacksStatus(stacks map[int]int) error {
	if !manager.isEnabled {
		return nil
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

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
		if manager.engineType == engineTypeKubernetes {
			fileName = fmt.Sprintf("%s.yml", stack.Name)
		}
		if manager.engineType == engineTypeNomad {
			fileName = fmt.Sprintf("%s.hcl", stack.Name)
		}

		err = filesystem.WriteFile(folder, fileName, []byte(stackConfig.FileContent), 644)
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

func (manager *StackManager) stop() error {
	if manager.stopSignal != nil {
		close(manager.stopSignal)
		manager.stopSignal = nil
		manager.isEnabled = false
	}

	return nil
}

func (manager *StackManager) start() error {
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

func (manager *StackManager) next() *edgeStack {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	for _, stack := range manager.stacks {
		if stack.Status == statusPending {
			return stack
		}
	}
	return nil
}

func (manager *StackManager) setEngineStatus(engineStatus engineType) error {
	if engineStatus == manager.engineType {
		return nil
	}

	manager.engineType = engineStatus

	err := manager.stop()
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
	manager.mu.Lock()
	defer manager.mu.Unlock()

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

	// Remove stack file folder
	err = os.RemoveAll(filepath.Dir(stackFileLocation))
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to delete Edge stack file] [error: %s]", err)
		return
	}

	manager.mu.Lock()
	delete(manager.stacks, stack.ID)
	manager.mu.Unlock()
}

func buildDeployerService(engineStatus engineType) (agent.Deployer, error) {
	switch engineStatus {
	case engineTypeDockerStandalone:
		return exec.NewDockerComposeStackService(agent.DockerBinaryPath)
	case engineTypeDockerSwarm:
		return exec.NewDockerSwarmStackService(agent.DockerBinaryPath)
	case engineTypeKubernetes:
		return exec.NewKubernetesDeployer(agent.DockerBinaryPath), nil
	case engineTypeNomad:
		return nomad.NewDeployer()
	}

	return nil, fmt.Errorf("engine status %d not supported", engineStatus)
}
