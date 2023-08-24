package docker

import (
	"context"

	"github.com/docker/docker/api/types"
)

func ImageDelete(name string, opts types.ImageRemoveOptions) ([]types.ImageDeleteResponseItem, error) {
	cli, err := NewClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	return cli.ImageRemove(context.Background(), name, opts)
}
