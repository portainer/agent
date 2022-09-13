package edge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockercli "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/portainer/api/edgetypes"
)

func (manager *Manager) updateAgent(version string, updateScheduleId edgetypes.UpdateScheduleID) error {
	if version == "" {
		return errors.New("version is required")
	}

	// TODO: REVIEW
	// Context should be handled properly
	ctx := context.TODO()

	// We create a Docker client to orchestrate docker operations
	cli, err := dockercli.NewClientWithOpts(dockercli.FromEnv, dockercli.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to create Docker client] [error: %s]", err)
		return err
	}
	defer cli.Close()

	log.Printf("[INFO] [edge] [message: starting agent update process] [version: %s]", version)

	if os.Getenv("SKIP_UPDATER_IMAGE_PULL") == "" {
		log.Printf("[DEBUG] [edge] [message: pulling latest portainer-updater image]")
		err = pullUpdaterImage(ctx, cli)
		if err != nil {
			return errors.WithMessage(err, "unable to pull portainer-updater image")
		}
	}

	log.Printf("[DEBUG] [edge] [message: retrieving agent container ID]")
	agentContainerId, err := getAgentContainerId()
	if err != nil {
		return errors.WithMessage(err, "unable to retrieve agent container ID")
	}

	log.Printf("[DEBUG] [edge] [message: running portainer-updater container]")

	updaterContainerId, err := runUpdate(ctx, cli, agentContainerId, version, updateScheduleId)

	// In case of a successful update, this code will not be reached
	// This is because the agent will be deleted at that point in time

	// TODO: We will need to find a way to trigger a clean-up process of the portainer-updater service container
	// Maybe after the agent starts, it could check for any existing stopped portainer-updater service container and remove it

	// everything after this line runs only if update failed (on success it will start a new container)
	defer clean(ctx, cli, updaterContainerId)
	if err != nil {
		return errors.WithMessage(err, "update failed")
	}

	_, err = printLogsToStdout(ctx, cli, updaterContainerId)
	if err != nil {
		return errors.WithMessage(err, "unable to print logs to stdout")
	}

	return errors.Errorf("update container exited and didn't update, see logs for more information")
}

func pullUpdaterImage(ctx context.Context, cli *dockercli.Client) error {
	// We make sure that the latest version of the portainer-updater image is available
	reader, err := cli.ImagePull(ctx, "portainer/portainer-updater:latest", types.ImagePullOptions{})
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to pull portainer-updater Docker image] [error: %s]", err)
		return err
	}
	defer reader.Close()

	// We have to read the content of the reader otherwise the image pulling process will be asynchronous
	// This is not really well documented by the Docker SDK
	io.Copy(io.Discard, reader)

	return nil
}

func getAgentContainerId() (string, error) {
	// Agent needs to retrieve its own container name to be passed to the portainer-updater service container

	// Unless overridden, the container hostname is matching the container ID
	// See https://stackoverflow.com/a/38983893

	// That could be achieved through:
	// portainerAgentContainerID, _ := os.Hostname()

	// BUT If the hostname property is set when creating the container
	// we can find ourselves in a situation where the container hostname is set to portainer_agent for example
	// but the container name / container ID is different
	// Therefore the approach of looking up the hostname is not enough.

	// Instead, we do a lookup in the /proc/1/cpuset file inside the container to find the container ID
	// See https://stackoverflow.com/a/63145861 and https://stackoverflow.com/a/25729598

	// TODO: REVIEW
	// This however will only work on Linux systems. I don't know if there is a way to do the same
	// inside a Windows container. In that case, we could fallback to the container hostname approach
	// and explicitly not support setting the hostname property on the agent container on Windows.
	cpuSetFileContent, err := os.ReadFile("/proc/1/cpuset")
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to read from /proc/1/cpuset to retrieve agent container name] [error: %s]", err)
		return "", err
	}

	// The content of that file looks like
	// /docker/<container ID>
	return strings.TrimSpace(strings.TrimPrefix(string(cpuSetFileContent), "/docker/")), nil

}

func runUpdate(ctx context.Context, cli *dockercli.Client, agentContainerId string, version string, updateScheduleId edgetypes.UpdateScheduleID) (string, error) {
	log.Printf("[DEBUG] [edge] [message: creating portainer-updater container]")

	agentImagePrefix := os.Getenv("AGENT_IMAGE_PREFIX")
	if agentImagePrefix == "" {
		agentImagePrefix = "portainer/agent"
	}

	agentImage := fmt.Sprintf("%s:%s", agentImagePrefix, version)

	updaterContainer, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: "portainer/portainer-updater:latest",
			Cmd:   []string{"agent-update", agentContainerId, agentImage, fmt.Sprintf("%d", updateScheduleId)},
			Env:   []string{fmt.Sprintf("SKIP_PULL=%s", os.Getenv("SKIP_UPDATER_IMAGE_PULL"))},
		},
		&container.HostConfig{
			Binds: []string{
				// TODO: REVIEW
				// This implementation will only work on Linux filesystems
				// For Windows, use a named pipe approach
				"/var/run/docker.sock:/var/run/docker.sock",
			},
		},
		nil, nil, fmt.Sprintf("portainer-updater-%s", uuid.New()))

	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to create portainer-updater container] [error: %s]", err)
		return "", err
	}

	log.Printf("[DEBUG] [edge] [message: starting portainer-updater container]")
	// We then start the portainer-updater service container
	err = cli.ContainerStart(ctx, updaterContainer.ID, types.ContainerStartOptions{})
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to start portainer-updater container] [error: %s]", err)
		return updaterContainer.ID, err
	}

	log.Printf("[DEBUG] [edge] [message: waiting for portainer-updater container to exit]")
	// We wait for it to finish
	statusCh, errCh := cli.ContainerWait(ctx, updaterContainer.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("[ERROR] [edge] [message: an error occurred while waiting for the upgrade of the agent through the portainer-updater service container] [error: %s]", err)
			return updaterContainer.ID, err
		}
	case <-statusCh:
	}

	return updaterContainer.ID, nil
}

func clean(ctx context.Context, cli *dockercli.Client, updaterContainerId string) {
	// The removal of the portainer-updater service container here is going to happen in the following cases:
	// * An error occurred during the update process
	// * The agent is already running the latest version of the image
	if updaterContainerId != "" {
		err := cli.ContainerRemove(ctx, updaterContainerId, types.ContainerRemoveOptions{})
		if err != nil {
			log.Printf("[ERROR] [edge] [message: unable to remove portainer-updater container] [error: %s]", err)
		}
	}
}

func printLogsToStdout(ctx context.Context, cli *dockercli.Client, updaterContainerId string) (string, error) {

	// We get the logs of the portainer-updater service container and write them to the agent output
	// Can be useful to troubleshoot the process in case of an update failure from the portainer-updater service container
	out, err := cli.ContainerLogs(ctx, updaterContainerId, types.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to get the portainer-updater container logs] [error: %s]", err)
		return "", err
	}

	var buf bytes.Buffer
	tee := io.TeeReader(out, &buf)

	// TODO: REVIEW
	// This could be something that we only output when the agent log level is set to DEBUG
	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, tee)
	if err != nil {
		log.Printf("[ERROR] [edge] [message: unable to copy the portainer-updater container logs to the agent output] [error: %s]", err)
	}

	return buf.String(), nil
}
