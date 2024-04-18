package updates

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/portainer/agent/docker"
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
	cli, err := docker.NewClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	foundRunningContainer := false

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "io.portainer.updater=true")),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %s", err.Error())
	}

	for _, c := range containers {
		if c.State == "exited" {
			err = cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
			if err != nil {
				return fmt.Errorf("failed to remove container: %s", err.Error())
			}

			if c.NetworkSettings != nil {
				for _, networkSetting := range c.NetworkSettings.Networks {
					err = cli.NetworkRemove(ctx, networkSetting.NetworkID)
					if err != nil {
						return fmt.Errorf("failed to remove network: %s", err.Error())
					}
				}
			}
		} else if c.State == "running" {
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
