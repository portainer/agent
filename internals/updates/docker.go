package updates

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type DockerUpdaterCleaner struct {
	updateID int
}

func NewDockerUpdaterCleaner(updateID int) *DockerUpdaterCleaner {
	return &DockerUpdaterCleaner{
		updateID: updateID,
	}
}

func (du *DockerUpdaterCleaner) Clean(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	foundRunningContainer := false

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "io.portainer.updater=true")),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %s", err.Error())
	}

	for _, container := range containers {
		if container.State == "exited" {
			err = cli.ContainerRemove(ctx, container.ID, types.ContainerRemoveOptions{Force: true})
			if err != nil {
				return fmt.Errorf("failed to remove container: %s", err.Error())
			}

			if container.NetworkSettings != nil {
				for _, networkSetting := range container.NetworkSettings.Networks {
					err = cli.NetworkRemove(ctx, networkSetting.NetworkID)
					if err != nil {
						return fmt.Errorf("failed to remove network: %s", err.Error())
					}
				}
			}
		} else if container.State == "running" {
			foundRunningContainer = true
		}
	}

	if foundRunningContainer {
		return errors.New("Found running updater container. Retry after 30 seconds.")
	}
	return nil
}

func (du *DockerUpdaterCleaner) UpdateID() int {
	return du.updateID
}
