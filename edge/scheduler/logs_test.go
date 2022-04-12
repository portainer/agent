package scheduler

import (
	"net/http"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
)

func TestDataRace(t *testing.T) {
	cli := client.NewPortainerClient(
		"portainerURL",
		"edgeID",
		func() string { return "endpointID" },
		agent.PlatformDocker,
		&http.Client{},
	)

	m := NewLogsManager(cli)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
