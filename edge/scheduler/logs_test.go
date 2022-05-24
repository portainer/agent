package scheduler

import (
	"net/http"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	portainer "github.com/portainer/portainer/api"
)

func TestDataRace(t *testing.T) {
	cli := client.NewPortainerClient(
		"portainerURL",
		func(portainer.EndpointID) {},
		func() portainer.EndpointID { return 1 },
		"edgeID",
		false,
		agent.PlatformDocker,
		&http.Client{},
	)

	m := NewLogsManager(cli)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
