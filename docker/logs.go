package docker

import (
	"bytes"
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/rs/zerolog/log"
)

func GetContainersWithLabel(value string) (r []types.Container, err error) {
	err = withCli(func(cli *client.Client) error {
		r, err = cli.ContainerList(context.Background(), container.ListOptions{
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

func GetContainerLogs(containerName, tail, since, until string) ([]byte, []byte, error) {
	cli, err := NewClient()
	if err != nil {
		return nil, nil, err
	}
	defer cli.Close()

	rd, err := cli.ContainerLogs(context.Background(), containerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
		Since:      since,
		Until:      until,
	})
	if err != nil {
		return nil, nil, err
	}
	defer rd.Close()

	var stdOut, stdErr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdOut, &stdErr, rd)

	return stdOut.Bytes(), stdErr.Bytes(), err
}

func GetLiveContainerLogs(containerName, since, until, tail string) (*io.PipeReader, *io.PipeReader, error) {
	cli, err := NewClientWithoutTimeout()
	if err != nil {
		return nil, nil, err
	}

	rd, err := cli.ContainerLogs(context.Background(), containerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Since:      since,
		Until:      until,
		Follow:     until != "",
		Tail:       tail,
	})
	if err != nil {
		_ = cli.Close()

		return nil, nil, err
	}

	stdOutR, stdOutW := io.Pipe()
	stdErrR, stdErrW := io.Pipe()

	go func() {
		if _, err := stdcopy.StdCopy(stdOutW, stdErrW, rd); err != nil {
			log.Warn().Err(err).Msg("error while reading container logs")
		}

		_ = rd.Close()
		_ = stdOutW.Close()
		_ = stdErrW.Close()
		_ = cli.Close()
	}()

	return stdOutR, stdErrR, nil
}
