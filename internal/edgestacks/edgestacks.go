package edgestacks

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/client"
)

const baseDir = "/tmp/edge_stacks"

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

// Manager represents a service for managing Edge stacks
type Manager struct {
	stacks             map[edgeStackID]*edgeStack
	stopSignal         chan struct{}
	dockerStackService agent.DockerStackService
	edgeID             string
	portainerURL       string
	endpointID         string
	isEnabled          bool
}

// NewManager creates a new instance of Manager
func NewManager(dockerStackService agent.DockerStackService, edgeID string) (*Manager, error) {
	return &Manager{
		dockerStackService: dockerStackService,
		edgeID:             edgeID,
		stacks:             map[edgeStackID]*edgeStack{},
		stopSignal:         nil,
	}, nil
}

// UpdateStacksStatus updates stacks version and status
func (manager *Manager) UpdateStacksStatus(stacks map[int]int) error {
	if !manager.isEnabled {
		return nil
	}

	for stackID, version := range stacks {
		stack, ok := manager.stacks[edgeStackID(stackID)]
		if ok {
			if stack.Version == version {
				continue
			}
			log.Printf("[DEBUG] [stacksmanager,update] [message: received stack to update %d] \n", stackID)

			stack.Action = actionUpdate
			stack.Version = version
			stack.Status = statusPending
		} else {
			log.Printf("[DEBUG] [stacksmanager,update] [message: received new stack %d] \n", stackID)

			stack = &edgeStack{
				Action:  actionDeploy,
				ID:      edgeStackID(stackID),
				Status:  statusPending,
				Version: version,
			}
		}

		cli, err := manager.createPortainerClient()
		if err != nil {
			return err
		}

		fileContent, prune, err := cli.GetEdgeStackConfig(int(stack.ID))
		if err != nil {
			return err
		}

		stack.Prune = prune

		folder := fmt.Sprintf("%s/%d", baseDir, stackID)
		fileName := "docker-compose.yml"
		err = filesystem.WriteFile(folder, fileName, []byte(fileContent), 644)
		if err != nil {
			return err
		}

		stack.FileFolder = folder
		stack.FileName = fileName

		manager.stacks[stack.ID] = stack

		err = cli.SetEdgeStackStatus(int(stack.ID), int(edgeStackStatusAcknowledged), "")
		if err != nil {
			return err
		}
	}

	for stackID, stack := range manager.stacks {
		if _, ok := stacks[int(stackID)]; !ok {
			log.Printf("[DEBUG] [stacksmanager,update] [message: received stack to delete %d] \n", stackID)
			stack.Action = actionDelete
			stack.Status = statusPending

			manager.stacks[stackID] = stack
		}
	}

	return nil
}

// Stop stops the manager
func (manager *Manager) Stop() error {
	if manager.stopSignal != nil {
		close(manager.stopSignal)
		manager.stopSignal = nil
		manager.isEnabled = false
	}

	return nil
}

// Start starts the loop checking for stacks to deploy
func (manager *Manager) Start(portainerURL, endpointID string) error {
	if manager.stopSignal != nil {
		return nil
	}

	manager.portainerURL = portainerURL
	manager.endpointID = endpointID
	manager.isEnabled = true
	manager.stopSignal = make(chan struct{})

	go (func() {
		for {
			select {
			case <-manager.stopSignal:
				log.Println("[DEBUG] [http,edge,stacksmanager] [message: shutting down Edge stack manager]")
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

func (manager *Manager) next() *edgeStack {
	for _, stack := range manager.stacks {
		if stack.Status == statusPending {
			return stack
		}
	}
	return nil
}

func (manager *Manager) deployStack(stack *edgeStack, stackName, stackFileLocation string) {
	log.Printf("[DEBUG] [stacksmanager,update] [message: deploying stack %d] \n", stack.ID)
	stack.Status = statusDone
	stack.Action = actionIdle
	responseStatus := int(edgeStackStatusOk)
	errorMessage := ""

	err := manager.dockerStackService.Deploy(stackName, stackFileLocation, stack.Prune)
	if err != nil {
		log.Printf("[ERROR] [http,edge,stacksmanager] [message: failed deploying stack] [error: %v] \n", err)
		stack.Status = statusError
		responseStatus = int(edgeStackStatusError)
		errorMessage = err.Error()
	} else {
		log.Printf("[DEBUG] [http,edge,stacksmanager] [message: deployed stack id: %v, version: %d] \n", stack.ID, stack.Version)
	}

	manager.stacks[stack.ID] = stack

	cli, err := manager.createPortainerClient()
	if err != nil {
		log.Printf("[ERROR] [http,edge,stacksmanager] [message: failed creating portainer client] [error: %v] \n", err)
	}

	err = cli.SetEdgeStackStatus(int(stack.ID), responseStatus, errorMessage)
	if err != nil {
		log.Printf("[ERROR] [http,edge,stacksmanager] [message: failed setting edge stack status] [error: %v] \n", err)
	}
}

func (manager *Manager) createPortainerClient() (*client.PortainerClient, error) {
	if manager.portainerURL == "" || manager.endpointID == "" || manager.edgeID == "" {
		return nil, errors.New("Client parameters are invalid")
	}
	return client.NewPortainerClient(manager.portainerURL, manager.endpointID, manager.edgeID), nil
}

func (manager *Manager) deleteStack(stack *edgeStack, stackName, stackFileLocation string) {
	log.Printf("[DEBUG] [stacksmanager,update] [message: removing stack %d] \n", stack.ID)
	err := filesystem.RemoveFile(stackFileLocation)
	if err != nil {
		log.Printf("[ERROR] [edge,stacksmanager, delete] [message: failed deleting edge stack file] [error: %v] \n", err)
		return
	}

	err = manager.dockerStackService.Remove(stackName)
	if err != nil {
		log.Printf("[ERROR] [edge,stacksmanager, delete] [message: failed removing stack] [error: %v] \n", err)
		return
	}

	delete(manager.stacks, stack.ID)
}
