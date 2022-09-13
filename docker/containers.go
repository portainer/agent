package docker

import (
	"context"

	"github.com/docker/docker/api/types"
)

func ContainerStart(name string, opts types.ContainerStartOptions) error {
	cli, err := getCli()
	if err != nil {
		return err
	}

	return cli.ContainerStart(context.Background(), name, opts)
}

func ContainerRestart(name string) error {
	cli, err := getCli()
	if err != nil {
		return err
	}

	return cli.ContainerRestart(context.Background(), name, nil)
}

func ContainerStop(name string) error {
	cli, err := getCli()
	if err != nil {
		return err
	}

	return cli.ContainerStop(context.Background(), name, nil)
}

func ContainerKill(name string) error {
	cli, err := getCli()
	if err != nil {
		return err
	}

	return cli.ContainerKill(context.Background(), name, "KILL")
}

func ContainerDelete(name string, opts types.ContainerRemoveOptions) error {
	cli, err := getCli()
	if err != nil {
		return err
	}

	return cli.ContainerRemove(context.Background(), name, opts)
}
