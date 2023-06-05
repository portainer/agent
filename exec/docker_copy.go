package exec

import (
	"fmt"
	"github.com/portainer/agent"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/portainer/agent/docker"
	"github.com/rs/zerolog/log"
)

// CopyToHostViaUnpacker copies src folder to composeDestination folder in the host
func CopyToHostViaUnpacker(src, dst string, stackID int, stackName, composeDestination, assetPath string) error {
	unpackerContainer, err := createUnpackerContainer(stackID, stackName, composeDestination)
	if err != nil {
		return err
	}

	err = copyToContainer(assetPath, src, unpackerContainer.ID, dst)
	if err != nil {
		return err
	}

	err = docker.ContainerDelete(unpackerContainer.ID, types.ContainerRemoveOptions{})
	return err
}

func getUnpackerImage() string {
	image := os.Getenv(agent.ComposeUnpackerImageEnvVar)
	if image == "" {
		image = agent.DefaultUnpackerImage
	}

	return image
}

func createUnpackerContainer(stackID int, stackName, composeDestination string) (container.CreateResponse, error) {
	image := getUnpackerImage()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	containerName := fmt.Sprintf("portainer-unpacker-%d-%s-%d", stackID, stackName, r.Intn(100))

	return docker.ContainerCreate(
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
