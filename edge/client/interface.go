package client

import (
	"net/http"
	"time"

	portainer "github.com/portainer/portainer/api"

	"github.com/portainer/agent"
)

type PortainerClient interface {
	GetEnvironmentID() (portainer.EndpointID, error)
	GetEnvironmentStatus() (*PollStatusResponse, error)
	GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error)
	SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error
	DeleteEdgeStackStatus(edgeStackID int) error
	SetEdgeJobStatus(edgeJobStatus agent.EdgeJobStatus) error
	SetTimeout(t time.Duration)
	SetLastCommandTimestamp(timestamp time.Time)
}

type PollStatusResponse struct {
	Status          string           `json:"status"`
	Port            int              `json:"port"`
	Schedules       []agent.Schedule `json:"schedules"`
	CheckinInterval float64          `json:"checkin"`
	Credentials     string           `json:"credentials"`
	Stacks          []StackStatus    `json:"stacks"`
	AsyncCommands   []AsyncCommand   `json:"commands"` // async mode only
}

type StackStatus struct {
	ID               int
	Version          int
	Name             string // used in async mode
	FileContent      string // used in async mode
	CommandOperation string // used in async mode
}

// NewPortainerClient returns a pointer to a new PortainerClient instance
func NewPortainerClient(serverAddress string, getEndpointIDFn func() portainer.EndpointID, edgeID string, edgeAsyncMode bool, agentPlatform agent.ContainerPlatform, httpClient *http.Client) PortainerClient {
	if edgeAsyncMode {
		return NewPortainerAsyncClient(serverAddress, getEndpointIDFn, edgeID, agentPlatform, httpClient)
	}

	return NewPortainerEdgeClient(serverAddress, getEndpointIDFn, edgeID, agentPlatform, httpClient)
}
