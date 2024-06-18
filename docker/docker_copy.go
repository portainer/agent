package docker

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"time"

	"github.com/portainer/agent"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
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

func buildRemoveDirCmd(src, dst string) []string {
	gitStackPath := filepath.Join(dst, filepath.Base(src))

	return []string{"remove-dir", gitStackPath}
}

// removeAndCopy removes the copy of src folder on the host,
// then copies src folder to the dst folder on the host
func removeAndCopy(src, dst string, stackID int, stackName, assetPath string, needCopy bool) error {
	if err := pullUnpackerImage(); err != nil {
		return err
	}

	removeDirCmd := buildRemoveDirCmd(src, dst)

	unpackerContainer, err := createUnpackerContainer(stackID, stackName, dst, removeDirCmd)
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
	image := getUnpackerImage()

	reader, err := ImagePull(image, types.ImagePullOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to pull unpacker image")
	}

	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)

	return nil
}

func createUnpackerContainer(stackID int, stackName, composeDestination string, cmd []string) (container.CreateResponse, error) {
	image := getUnpackerImage()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	containerName := fmt.Sprintf("portainer-unpacker-%d-%s-%d", stackID, stackName, r.Intn(100))

	return ContainerCreate(
		&container.Config{
			Image: image,
			Cmd:   cmd,
		},
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
