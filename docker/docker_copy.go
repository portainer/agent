package docker

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"

	"github.com/portainer/agent"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/portainer/portainer/pkg/librand"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// CopyGitStackToHost copies src folder to the dst folder on the host
func CopyGitStackToHost(src, dst string, stackID int, stackName, assetPath string) error {
	return removeAndCopy(src, dst, stackID, stackName, assetPath, true)
}

// RemoveGitStackFromHost removes the copy of src folder on the host
func RemoveGitStackFromHost(src, dst string, stackID int, stackName string) error {
	return removeAndCopy(src, dst, stackID, stackName, "", false)
}

func buildRemoveDirCmd(src, dst string, fips bool) []string {
	var cmd []string
	if fips {
		cmd = append(cmd, "--fips-mode")
	}

	gitStackPath := filepath.Join(dst, filepath.Base(src))
	cmd = append(cmd, []string{"remove-dir", gitStackPath}...)

	return cmd
}

// removeAndCopy removes the copy of src folder on the host,
// then copies src folder to the dst folder on the host
func removeAndCopy(src, dst string, stackID int, stackName, assetPath string, needCopy bool) error {
	if err := pullUnpackerImage(); err != nil {
		return err
	}

	fips := fips.FIPSMode()

	removeDirCmd := buildRemoveDirCmd(src, dst, fips)

	unpackerContainer, err := createUnpackerContainer(stackID, stackName, dst, removeDirCmd, fips)
	if err != nil {
		return err
	}

	defer removeUnpackerContainer(unpackerContainer)

	if err := ContainerStart(unpackerContainer.ID, container.StartOptions{}); err != nil {
		return err
	}

	statusCh, errCh := ContainerWait(unpackerContainer.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-statusCh:
	}

	if needCopy {
		return copyToContainer(assetPath, src, unpackerContainer.ID, dst)
	}

	return nil
}

func removeUnpackerContainer(unpackerContainer container.CreateResponse) error {
	if err := ContainerDelete(unpackerContainer.ID, container.RemoveOptions{}); err != nil {
		log.Error().
			Str("ContainerID", unpackerContainer.ID).
			Msg("Failed to remove unpacker container")

		return err
	}

	return nil
}

func getUnpackerImage() string {
	if image := os.Getenv(agent.ComposeUnpackerImageEnvVar); image != "" {
		return image
	}

	return agent.DefaultUnpackerImage
}

func pullUnpackerImage() error {
	unpackerImg := getUnpackerImage()

	reader, err := ImagePull(unpackerImg, image.PullOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to pull unpacker image")
	}

	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)

	return nil
}

func createContainerConfig(cmd []string, fips bool) *container.Config {
	image := getUnpackerImage()

	containerConfig := &container.Config{
		Image: image,
		Cmd:   cmd,
	}

	if fips {
		containerConfig.Env = []string{"GODEBUG=fips140=on"}
	}

	return containerConfig
}

func createUnpackerContainer(stackID int, stackName, composeDestination string, cmd []string, fips bool) (container.CreateResponse, error) {
	containerName := "portainer-unpacker-" + strconv.Itoa(stackID) + "-" + stackName + "-" + strconv.Itoa(librand.Intn(100))

	containerConfig := createContainerConfig(cmd, fips)

	return ContainerCreate(
		containerConfig,
		&container.HostConfig{
			Binds: []string{
				fmt.Sprintf("%s:%s", composeDestination, composeDestination),
			},
		},
		nil,
		nil,
		containerName,
	)
}

func copyToContainer(assetPath, src, containerID, dst string) error {
	dockerBinaryPath := path.Join(assetPath, "docker")
	fullDst := containerID + ":" + dst
	cmd := exec.Command(dockerBinaryPath, "cp", src, fullDst)

	output, err := cmd.Output()
	if err != nil {
		return err
	}

	log.Debug().Str("output", string(output)).Msg("Copy stack to host filesystem")

	return nil
}
