package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func ImageDelete(name string, opts types.ImageRemoveOptions) ([]types.ImageDeleteResponseItem, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	return cli.ImageRemove(context.Background(), name, opts)
}
