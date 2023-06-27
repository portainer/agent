package docker

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/rs/zerolog/log"
)

// CopyToHostViaUnpacker copies src folder of agent container into the dst folder of the host
func CopyToHostViaUnpacker(src, dst string, stackID int, stackName, assetPath string) error {
	err := pullUnpackerImage()
	if err != nil {
		return err
	}

	unpackerContainer, err := createUnpackerContainer(stackID, stackName, dst)
	if err != nil {
		return err
	}

	err = copyToContainer(assetPath, src, unpackerContainer.ID, dst)
	if err != nil {
		return err
	}

	err = ContainerDelete(unpackerContainer.ID, types.ContainerRemoveOptions{})
	return err
}

func getUnpackerImage() string {
	image := os.Getenv(agent.ComposeUnpackerImageEnvVar)
	if image == "" {
		image = agent.DefaultUnpackerImage
	}

	return image
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

func createUnpackerContainer(stackID int, stackName, composeDestination string) (container.CreateResponse, error) {
	image := getUnpackerImage()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	containerName := fmt.Sprintf("portainer-unpacker-%d-%s-%d", stackID, stackName, r.Intn(100))

	return ContainerCreate(
		&container.Config{
			Image: image,
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
	fullDst := fmt.Sprintf("%s:%s", containerID, dst)
	cmd := exec.Command(dockerBinaryPath, "cp", src, fullDst)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	log.Debug().Str("output", string(output)).Msg("Copy stack to host filesystem")
	return nil
}
