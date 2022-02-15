package client

import (
	"net/http"
	"time"

	"github.com/portainer/agent"
)

type PortainerClient interface {
	GetEnvironmentStatus() (*PollStatusResponse, error)
	GetEdgeStackConfig(edgeStackID int) (*agent.EdgeStackConfig, error)
	SetEdgeStackStatus(edgeStackID, edgeStackStatus int, error string) error
	SendJobLogFile(jobID int, fileContent []byte) error
	SetTimeout(t time.Duration)
}

type StackStatus struct {
	ID      int
	Version int
}

type PollStatusResponse struct {
	Status          string           `json:"status"`
	Port            int              `json:"port"`
	Schedules       []agent.Schedule `json:"schedules"`
	CheckinInterval float64          `json:"checkin"`
	Credentials     string           `json:"credentials"`
	Stacks          []StackStatus    `json:"stacks"`
}

type stackConfigResponse struct {
	Name             string
	StackFileContent string
	ImageMapping     map[string]string // a map of stackfile image to imageCache url(with sha)
}

type setEdgeStackStatusPayload struct {
	Error      string
	Status     int
	EndpointID int
}

type logFilePayload struct {
	FileContent string
}

// NewPortainerClient returns a pointer to a new PortainerClient instance
func NewPortainerClient(serverAddress, endpointID, edgeID string, asyncEdgeMode bool, containerPlatform agent.ContainerPlatform, httpClient *http.Client) PortainerClient {
	if asyncEdgeMode {
		return NewPortainerAsyncClient(serverAddress, endpointID, edgeID, containerPlatform, httpClient)
	}
	return NewPortainerEdgeClient(serverAddress, endpointID, edgeID, containerPlatform, httpClient)
}
