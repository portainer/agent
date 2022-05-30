package docker

import (
	"bytes"
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/portainer/agent"
)

func GetContainersWithLabel(value string) ([]types.Container, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	return cli.ContainerList(context.Background(), types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: value,
		}),
	})
}

func GetContainerLogs(containerName string, tail string) ([]byte, []byte, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, nil, err
	}
	defer cli.Close()

	rd, err := cli.ContainerLogs(context.Background(), containerName, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		return nil, nil, err
	}
	defer rd.Close()

	var stdOut, stdErr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdOut, &stdErr, rd)

	return stdOut.Bytes(), stdErr.Bytes(), err
}
