package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

func ContainerCreate(
	config *container.Config,
	hostConfig *container.HostConfig,
	networkingConfig *network.NetworkingConfig,
	platform *specs.Platform,
	containerName string,
) (container.CreateResponse, error) {
	var cli *client.Client

	err := withCli(func(cliInner *client.Client) error {
		cli = cliInner
		return nil
	})
	if err != nil {
		return container.CreateResponse{}, err
	}

	return cli.ContainerCreate(context.Background(), config, hostConfig, networkingConfig, platform, containerName)
}

func ContainerStart(name string, opts types.ContainerStartOptions) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerStart(context.Background(), name, opts)
	})
}

func ContainerRestart(name string) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerRestart(context.Background(), name, container.StopOptions{})
	})
}

func ContainerStop(name string) error {
	return withCli(func(cli *client.Client) error {
		return cli.ContainerStop(context.Background(), name, container.StopOptions{})
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
