package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func ContainerStart(name string, opts types.ContainerStartOptions) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerStart(context.Background(), name, opts)
	})
}

func ContainerRestart(name string) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerRestart(context.Background(), name, nil)
	})
}

func ContainerStop(name string) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerStop(context.Background(), name, nil)
	})
}

func ContainerKill(name string) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerKill(context.Background(), name, "KILL")
	})
}

func ContainerDelete(name string, opts types.ContainerRemoveOptions) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerRemove(context.Background(), name, opts)
	})
}
