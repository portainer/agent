package client

import (
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"

	"github.com/portainer/agent"
)

const (
	EdgeConfigIdleState EdgeConfigStateType = iota
	EdgeConfigFailureState
	EdgeConfigSavingState
	EdgeConfigDeletingState
	EdgeConfigUpdatingState
)

type PortainerClient interface {
	GetEnvironmentID() (portainer.EndpointID, error)
	GetEnvironmentStatus(flags ...string) (*PollStatusResponse, error)
	GetEdgeStackConfig(edgeStackID int, version *int) (*edge.StackPayload, error)
	SetEdgeStackStatus(edgeStackID int, edgeStackStatus portainer.EdgeStackStatusType, rollbackTo *int, error string) error
	SetEdgeJobStatus(edgeJobStatus agent.EdgeJobStatus) error
	GetEdgeConfig(id EdgeConfigID) (*EdgeConfig, error)
	SetEdgeConfigState(id EdgeConfigID, state EdgeConfigStateType) error
	SetTimeout(t time.Duration)
	SetLastCommandTimestamp(timestamp time.Time)
	EnqueueLogCollectionForStack(logCmd LogCommandData) error
}

type EdgeConfigID int
type EdgeConfigStateType int

func (e EdgeConfigStateType) String() string {
	switch e {
	case EdgeConfigIdleState:
		return "Idle"
	case EdgeConfigFailureState:
		return "Failure"
	case EdgeConfigSavingState:
		return "Saving"
	case EdgeConfigDeletingState:
		return "Deleting"
	case EdgeConfigUpdatingState:
		return "Updating"
	}

	return "N/A"
}

type EdgeConfig struct {
	ID         EdgeConfigID
	Name       string
	BaseDir    string
	DirEntries []filesystem.DirEntry
	Prev       *EdgeConfig
}

type PollStatusResponse struct {
	Status             string                               `json:"status"`
	Port               int                                  `json:"port"`
	Schedules          []agent.Schedule                     `json:"schedules"`
	CheckinInterval    float64                              `json:"checkin"`
	Credentials        string                               `json:"credentials"`
	Stacks             []StackStatus                        `json:"stacks"`
	EdgeConfigurations map[EdgeConfigID]EdgeConfigStateType `json:"edge_configurations"`

	// Async mode only
	EndpointID       int            `json:"endpointID"`
	PingInterval     time.Duration  `json:"pingInterval"`
	SnapshotInterval time.Duration  `json:"snapshotInterval"`
	CommandInterval  time.Duration  `json:"commandInterval"`
	AsyncCommands    []AsyncCommand `json:"commands"`
}

type StackStatus struct {
	ID               int
	Version          int
	Name             string // used in async mode
	CommandOperation string // used in async mode
}

type setEndpointIDFn func(portainer.EndpointID)
type getEndpointIDFn func() portainer.EndpointID

// NewPortainerClient returns a pointer to a new PortainerClient instance
func NewPortainerClient(serverAddress string, setEIDFn setEndpointIDFn, getEIDFn getEndpointIDFn, edgeID string, edgeAsyncMode bool, agentPlatform agent.ContainerPlatform, metaFields agent.EdgeMetaFields, httpClient *edgeHTTPClient) PortainerClient {
	if edgeAsyncMode {
		return NewPortainerAsyncClient(serverAddress, setEIDFn, getEIDFn, edgeID, agentPlatform, metaFields, httpClient)
	}

	return NewPortainerEdgeClient(serverAddress, setEIDFn, getEIDFn, edgeID, agentPlatform, metaFields, httpClient)
}
