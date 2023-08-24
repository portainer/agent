package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func ImageDelete(name string, opts types.ImageRemoveOptions) (r []types.ImageDeleteResponseItem, err error) {
	err = withCli(func(cli *client.Client) error {
		r, err = cli.ImageRemove(context.Background(), name, opts)

		return err
	})

	return r, err
}
