package scheduler

import (
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
)

func TestDataRace(t *testing.T) {
	fips.InitFIPS(false)

	cli := client.NewPortainerClient(
		"portainerURL",
		func(portainer.EndpointID) {},
		func() portainer.EndpointID { return 1 },
		"edgeID",
		"edgeKey",
		false,
		agent.PlatformDocker,
		agent.EdgeMetaFields{},
		client.BuildHTTPClient(10, &agent.Options{}),
	)

	m := NewLogsManager(cli)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
