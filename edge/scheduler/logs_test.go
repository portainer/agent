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
		1,
		"edgeID",
		false,
		agent.PlatformDocker,
		&http.Client{},
	)

	m := NewLogsManager(cli)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
