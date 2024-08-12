package updates

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/portainer/agent/docker"
	"github.com/rs/zerolog/log"
)

type DockerUpdaterCleaner struct {
	updateID int
}

func NewDockerUpdaterCleaner(ctx context.Context, updateID int) *DockerUpdaterCleaner {
	if err := updateAgentInfoIfNeeds(ctx, updateID); err != nil {
		log.Warn().Err(err).
			Str("context", " UpdateAgentInfoIfNeeds").
			Msg("UpdatedAgent fails to update the agents information")
	}
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

			log.Debug().Strs("container_name", c.Names).
				Str("state", c.State).
				Str("id", c.ID).
				Msg("Agent removes the updater container successfully")

		} else if c.State == "running" {
			foundRunningContainer = true
		}
	}

	if foundRunningContainer {
		return errors.New("found running updater container. Retry after 30 seconds")
	}
	return nil
}

func (du *DockerUpdaterCleaner) UpdateID() int {
	return du.updateID
}

func updateAgentInfoIfNeeds(ctx context.Context, updateID int) error {
	cli, err := docker.NewClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	containers, err := getAgentContainerCandicates(ctx, cli)
	if err != nil {
		return fmt.Errorf("unable to list containers. Error: %w", err)
	}

	var (
		oldAgentContainer *types.Container
		newAgentContainer *types.Container
		oldContainerName  string
	)
	for i, container := range containers {
		if container.Labels != nil && container.Labels["io.portainer.update.scheduleId"] == strconv.Itoa(updateID) {
			newAgentContainer = containers[i]
			continue
		}

		oldAgentContainer = containers[i]
		if len(oldAgentContainer.Names) > 0 {
			oldContainerName = strings.TrimPrefix(oldAgentContainer.Names[0], "/")
		}
	}

	// Check if the old agent exists
	if oldAgentContainer != nil {
		if err := tryRemoveOldContainer(ctx, cli, oldAgentContainer.ID); err != nil {
			log.Warn().Err(err).
				Str("old_agent_container_id", oldAgentContainer.ID).
				Str("old_agent_container_status", oldAgentContainer.Status).
				Str("old_agent_container_state", oldAgentContainer.State).
				Str("context", "NewAgentRemovesOldAgent").
				Msg("UpdatedAgent fails to remove old agent container")
		} else {
			log.Info().
				Str("old_agent_container_id", oldAgentContainer.ID).
				Str("old_agent_name", oldContainerName).
				Str("context", "NewAgentRemovesOldAgent").
				Msg("UpdatedAgent removed old agent container successfully")
		}
	}

	// Check if the new agent name is formal
	if newAgentContainer != nil {
		shouldRename := false
		for _, name := range newAgentContainer.Names {
			log.Debug().
				Str("name", name).
				Msg("new agent name")
			if strings.HasSuffix(name, "-update") {
				shouldRename = true
				break
			}
		}

		if shouldRename {
			// rename new container to old container name
			if err := cli.ContainerRename(ctx, newAgentContainer.ID, oldContainerName); err != nil {
				log.Warn().Err(err).
					Str("updated_agent_container_id", newAgentContainer.ID).
					Strs("updated_agent_container_name", newAgentContainer.Names).
					Str("old_agent_container_name", oldContainerName).
					Str("context", "RenameNewAgentContainer").
					Msg("Unable to rename container")
			} else {
				log.Info().
					Str("updated_agent_container_id", newAgentContainer.ID).
					Str("updated_agent_container_name", oldContainerName).
					Str("context", "RenameNewAgentContainer").
					Msg("UpdatedAgent is renamed successfully")
			}
		}
	}

	return nil
}

func tryRemoveOldContainer(ctx context.Context, dockerCli *client.Client, oldContainerId string) error {
	log.Debug().
		Str("containerId", oldContainerId).
		Msg("Removing old container")

	// remove old container
	return dockerCli.ContainerRemove(ctx, oldContainerId, container.RemoveOptions{Force: true})
}

func getAgentContainerCandicates(ctx context.Context, cli *client.Client) ([]*types.Container, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("unable to list containers. Error: %w", err)
	}

	uniqueContainers := map[string]*types.Container{}
	// Filter by label
	possibleLabel := "io.portainer.agent"
	for i, container := range containers {
		if container.Labels != nil && container.Labels[possibleLabel] == "true" {
			uniqueContainers[container.ID] = &containers[i]
		}
	}

	// If filtering by label failed (the old version agent might not be added label), filter by possible image name.
	possibleImagePrefixes := []string{"portainer/agent", "portainerci/agent"}
	for i, container := range containers {
		for _, possibleImage := range possibleImagePrefixes {
			if strings.HasPrefix(container.Image, possibleImage) {
				uniqueContainers[container.ID] = &containers[i]
			}
		}
	}

	// If filter by label and image failed, filter by logs
	possibleLog := "Starting Agent API server"
	for i, container := range containers {
		logs, err := cli.ContainerLogs(ctx, container.ID, dockercontainer.LogsOptions{
			ShowStdout: true,
			ShowStderr: false,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to get container logs. Error: %w", err)
		}

		scanner := bufio.NewScanner(logs)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), possibleLog) {
				uniqueContainers[container.ID] = &containers[i]
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("unable to read container logs. Error: %w", err)
		}
	}

	containerCandicates := []*types.Container{}
	for _, container := range uniqueContainers {
		containerCandicates = append(containerCandicates, container)
	}
	return containerCandicates, nil
}
