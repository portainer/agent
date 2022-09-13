package docker

import (
	"context"

	"github.com/docker/docker/api/types"
)

func ImageDelete(name string, opts types.ImageRemoveOptions) ([]types.ImageDeleteResponseItem, error) {
	cli, err := getCli()
	if err != nil {
		return nil, err
	}

	return cli.ImageRemove(context.Background(), name, opts)
}
