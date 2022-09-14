package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/portainer/agent"
)

func ImageDelete(name string, opts types.ImageRemoveOptions) ([]types.ImageDeleteResponseItem, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	return cli.ImageRemove(context.Background(), name, opts)
}
