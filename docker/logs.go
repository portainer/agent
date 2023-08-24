package docker

import (
	"bytes"
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func GetContainersWithLabel(value string) (r []types.Container, err error) {
	err = withCli(func(cli *client.Client) error {
		r, err = cli.ContainerList(context.Background(), types.ContainerListOptions{
			All: true,
			Filters: filters.NewArgs(filters.KeyValuePair{
				Key:   "label",
				Value: value,
			}),
		})

		return err
	})

	return r, err
}

func GetContainerLogs(containerName string, tail string) ([]byte, []byte, error) {
	cli, err := NewClient()
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
