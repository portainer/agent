package updates

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/portainer/agent"
)

type DockerUpdaterCleaner struct {
}

func NewDockerUpdaterCleaner() *DockerUpdaterCleaner {
	return &DockerUpdaterCleaner{}
}

func (du *DockerUpdaterCleaner) Clean(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return err
	}
	defer cli.Close()

	foundRunningContainer := false
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %s", err.Error())
	}
	for _, container := range containers {
		_, ok := container.Labels["io.portainer.hideStack"]
		if ok {
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
	}

	if foundRunningContainer {
		return errors.New("Found running updater container. Retry after 30 seconds.")
	}
	return nil
}
