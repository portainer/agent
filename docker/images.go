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

func ImageInspect(name string) (*types.ImageInspect, error) {
	var inspect *types.ImageInspect

	err := withCli(func(cli *client.Client) error {
		r, _, err := cli.ImageInspectWithRaw(context.Background(), name)
		if err != nil {
			return err
		}

		inspect = &r

		return nil
	})

	return inspect, err
}
