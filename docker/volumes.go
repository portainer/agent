package docker

import (
	"context"

	"github.com/docker/docker/client"
)

func VolumeDelete(name string, force bool) error {
	return withCli(func(cli *client.Client) error {
		return cli.VolumeRemove(context.Background(), name, force)
	})
}
