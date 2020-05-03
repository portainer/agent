package edgestacks

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/portainerclient"
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

// EdgeStackManager represents a service for managing Edge stacks
type EdgeStackManager struct {
	stacks             map[edgeStackID]*edgeStack
	stopSignal         chan struct{}
	dockerStackService agent.DockerStackService
	keyService         agent.EdgeKeyService
	edgeID             string
}

// NewManager creates a new instance of EdgeStackManager
func NewManager(dockerStackService agent.DockerStackService, keyService agent.EdgeKeyService, edgeID string) (*EdgeStackManager, error) {
	return &EdgeStackManager{
		dockerStackService: dockerStackService,
		keyService:         keyService,
		edgeID:             edgeID,
		stacks:             map[edgeStackID]*edgeStack{},
		stopSignal:         nil,
	}, nil
}

// UpdateStacksStatus updates stacks version and status
func (manager *EdgeStackManager) UpdateStacksStatus(stacks map[int]int) error {
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
			stack.Action = actionDelete
			stack.Status = statusPending

			manager.stacks[stackID] = stack
		}
	}

	return nil
}

// Stop stops the manager
func (manager *EdgeStackManager) Stop() {
	if manager.stopSignal == nil {
		return
	}

	close(manager.stopSignal)
	manager.stopSignal = nil
}

// Start starts the loop checking for stacks to deploy
func (manager *EdgeStackManager) Start() error {
	if manager.stopSignal != nil {
		return errors.New("Manager is already started")
	}

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
		}
	})()

	return nil
}

func (manager *EdgeStackManager) next() *edgeStack {
	for _, stack := range manager.stacks {
		if stack.Status == statusPending {
			return stack
		}
	}
	return nil
}

func (manager *EdgeStackManager) createPortainerClient() (*portainerclient.PortainerClient, error) {
	portainerURL, endpointID, err := manager.keyService.GetPortainerConfig()
	if err != nil {
		return nil, err
	}

	return portainerclient.NewPortainerClient(portainerURL, endpointID, manager.edgeID), nil
}