package edge

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/client"
)

type edgeStackID int

type edgeStack struct {
	ID         edgeStackID
	Version    int
	FileFolder string
	FileName   string
	Prune      bool
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

// StacksManager represents a service for managing Edge stacks
type StacksManager struct {
	stacks             map[edgeStackID]*edgeStack
	stopSignal         chan struct{}
	dockerStackService agent.DockerStackService
	portainerURL       string
	endpointID         string
	isEnabled          bool
	httpClient         *client.PortainerClient
}

// NewStacksManager creates a new instance of StacksManager
func NewStacksManager(dockerStackService agent.DockerStackService, portainerURL, endpointID, edgeID string) (*StacksManager, error) {
	cli := client.NewPortainerClient(portainerURL, endpointID, edgeID)

	return &StacksManager{
		dockerStackService: dockerStackService,
		stacks:             map[edgeStackID]*edgeStack{},
		stopSignal:         nil,
		httpClient:         cli,
	}, nil
}

// UpdateStacksStatus updates stacks version and status
func (manager *StacksManager) UpdateStacksStatus(stacks map[int]int) error {
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

		fileContent, prune, err := manager.httpClient.GetEdgeStackConfig(int(stack.ID))
		if err != nil {
			return err
		}

		stack.Prune = prune

		folder := fmt.Sprintf("%s/%d", agent.EdgeStackFilesPath, stackID)
		fileName := "docker-compose.yml"
		err = filesystem.WriteFile(folder, fileName, []byte(fileContent), 644)
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

// Stop stops the manager
func (manager *StacksManager) Stop() error {
	if manager.stopSignal != nil {
		close(manager.stopSignal)
		manager.stopSignal = nil
		manager.isEnabled = false
	}

	return nil
}

// Start starts the loop checking for stacks to deploy
func (manager *StacksManager) Start() error {
	if manager.stopSignal != nil {
		return nil
	}

	manager.isEnabled = true
	manager.stopSignal = make(chan struct{})

	go (func() {
		for {
			select {
			case <-manager.stopSignal:
				log.Println("[DEBUG] [internal,edge,stack] [message: shutting down Edge stack manager]")
				return
			default:
				stack := manager.next()
				if stack == nil {
					timer1 := time.NewTimer(1 * time.Minute)
					<-timer1.C
					continue
				}

				stackName := fmt.Sprintf("edge_%d", stack.ID)
				stackFileLocation := fmt.Sprintf("%s/%s", stack.FileFolder, stack.FileName)

				if stack.Action == actionDeploy || stack.Action == actionUpdate {
					manager.deployStack(stack, stackName, stackFileLocation)
				} else if stack.Action == actionDelete {
					manager.deleteStack(stack, stackName, stackFileLocation)
				}
			}
		}
	})()

	return nil
}

func (manager *StacksManager) next() *edgeStack {
	for _, stack := range manager.stacks {
		if stack.Status == statusPending {
			return stack
		}
	}
	return nil
}

func (manager *StacksManager) deployStack(stack *edgeStack, stackName, stackFileLocation string) {
	log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: stack deployment]", stack.ID)
	stack.Status = statusDone
	stack.Action = actionIdle
	responseStatus := int(edgeStackStatusOk)
	errorMessage := ""

	err := manager.dockerStackService.Deploy(stackName, stackFileLocation, stack.Prune)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: stack deployment failed] [error: %s]", err)
		stack.Status = statusError
		responseStatus = int(edgeStackStatusError)
		errorMessage = err.Error()
	} else {
		log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: stack deployed]", stack.ID, stack.Version)
	}

	manager.stacks[stack.ID] = stack

	err = manager.httpClient.SetEdgeStackStatus(int(stack.ID), responseStatus, errorMessage)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to update Edge stack status] [error: %s]", err)
	}
}

func (manager *StacksManager) deleteStack(stack *edgeStack, stackName, stackFileLocation string) {
	log.Printf("[DEBUG] [internal,edge,stack] [stack_identifier: %d] [message: removing stack]", stack.ID)
	err := filesystem.RemoveFile(stackFileLocation)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to delete Edge stack file] [error: %s]", err)
		return
	}

	err = manager.dockerStackService.Remove(stackName)
	if err != nil {
		log.Printf("[ERROR] [internal,edge,stack] [message: unable to remove stack] [error: %s]", err)
		return
	}

	delete(manager.stacks, stack.ID)
}
