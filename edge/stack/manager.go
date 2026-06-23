package stack

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/portainer/api/edge"

	"github.com/rs/zerolog/log"
)

type engineType int

const (
	// TODO: consider defining this in agent.go or re-use/enhance some of the existing constants
	// that are declared in agent.go
	_ engineType = iota
	EngineTypeDockerStandalone
	EngineTypeDockerSwarm
	EngineTypeKubernetes
	// Deprecated
	EngineTypeNomad
)

const edgeUpdateStackNamePrefix = "edge-update-schedule-"

// StackManager represents a service for managing Edge stacks
type StackManager struct {
	engineType      engineType
	edgeID          string
	stacks          map[edgeStackID]*edgeStack
	stopSignal      chan struct{}
	deployer        deployer.Deployer
	isEnabled       bool
	portainerClient client.PortainerClient
	awsConfig       *agent.AWSConfig
	mu              sync.Mutex
	kubeClient      *kubernetes.KubeClient
}

// NewStackManager returns a pointer to a new instance of StackManager
func NewStackManager(cli client.PortainerClient, config *agent.AWSConfig, edgeID string, kubeClient *kubernetes.KubeClient) *StackManager {
	return &StackManager{
		stacks:          map[edgeStackID]*edgeStack{},
		stopSignal:      nil,
		portainerClient: cli,
		awsConfig:       config,
		edgeID:          edgeID,
		kubeClient:      kubeClient,
	}
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

func (manager *StackManager) Stop() {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.stopSignal != nil {
		close(manager.stopSignal)
		manager.stopSignal = nil
		manager.isEnabled = false
	}
}

func (manager *StackManager) ResetStacks() {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.stacks = map[edgeStackID]*edgeStack{}
}

func (manager *StackManager) SetEngineType(engineTyp engineType) error {
	if engineTyp == manager.engineType {
		return nil
	}

	manager.Stop()

	deployer, err := manager.buildDeployerService(engineTyp)
	if err != nil {
		return err
	}

	manager.engineType = engineTyp
	manager.deployer = deployer

	return nil
}

// LoadExistingEdgeStacks loads all the edge stacks deployed by Portainer
func (manager *StackManager) LoadExistingEdgeStacks(ctx context.Context) error {
	edgeStacks, err := manager.deployer.GetEdgeStacks(ctx)
	if err != nil {
		return err
	}

	manager.mu.Lock()
	for _, s := range edgeStacks {
		if _, found := manager.stacks[edgeStackID(s.ID)]; found {
			continue
		}

		edgeUpdateFailed := s.ExitCode != 0 && strings.HasPrefix(s.Name, edgeUpdateStackNamePrefix)

		manager.stacks[edgeStackID(s.ID)] = &edgeStack{
			StackPayload: edge.StackPayload{
				ID:   s.ID,
				Name: s.Name,
			},
			Action:           actionIdle,
			Status:           StatusPending,
			EdgeUpdateFailed: edgeUpdateFailed,
		}
	}
	manager.mu.Unlock()

	return nil
}

func (manager *StackManager) LoadExistingPortainerUpdaterEdgeStack(ctx context.Context) error {
	// Load the Portainer Updater Edge Stack if it exists
	edgeStacks, err := manager.deployer.GetEdgeStacks(ctx)
	if err != nil {
		return err
	}

	for _, s := range edgeStacks {
		edgeUpdateIDString, isPortainerUpdaterEdgeStack := strings.CutPrefix(s.Name, edgeUpdateStackNamePrefix)
		if isPortainerUpdaterEdgeStack {
			edgeUpdateID, err := strconv.Atoi(edgeUpdateIDString)
			if err != nil {
				log.Error().Err(err).Msgf("failed to parse edge update ID from stack name %s", s.Name)
				continue
			}

			manager.mu.Lock()
			manager.stacks[edgeStackID(s.ID)] = &edgeStack{
				StackPayload: edge.StackPayload{
					ID:           s.ID,
					Name:         s.Name,
					EdgeUpdateID: edgeUpdateID,
				},
				Action:           actionIdle,
				Status:           StatusPending,
				EdgeUpdateFailed: s.ExitCode != 0,
			}
			manager.mu.Unlock()
			log.Debug().Msg("successfully loaded portainer-updater edge stack")

			return nil
		}
	}

	return nil
}

func (manager *StackManager) buildDeployerService(engineStatus engineType) (deployer.Deployer, error) {
	switch engineStatus {
	case EngineTypeDockerStandalone:
		return exec.NewDockerComposeStackService(), nil
	case EngineTypeDockerSwarm:
		return exec.NewDockerSwarmStackService(), nil
	case EngineTypeKubernetes:
		return exec.NewKubernetesDeployer(manager.kubeClient), nil
	}

	return nil, fmt.Errorf("engine status %d not supported", engineStatus)
}
