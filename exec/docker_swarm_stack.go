package exec

import (
	"context"
	"errors"
	"path"
	"runtime"
)

// DockerSwarmStackService represents a service for managing stacks by using the Docker binary.
type DockerSwarmStackService struct {
	binaryPath string
}

type DockerSwarmDeployOpts struct {
	Prune bool
}

// NewDockerSwarmStackService initializes a new DockerStackService service.
// It also updates the configuration of the Docker CLI binary.
func NewDockerSwarmStackService(binaryPath string) (*DockerSwarmStackService, error) {
	service := &DockerSwarmStackService{
		binaryPath: binaryPath,
	}

	return service, nil
}

// Deploy executes the docker stack deploy command.
func (service *DockerSwarmStackService) Deploy(ctx context.Context, name string, filePaths []string, prune bool) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	command := service.prepareDockerCommand(service.binaryPath)

	args := []string{}
	if prune {
		args = append(args, "stack", "deploy", "--prune", "--with-registry-auth", "--compose-file", stackFilePath, name)
	} else {
		args = append(args, "stack", "deploy", "--with-registry-auth", "--compose-file", stackFilePath, name)
	}

	stackFolder := path.Dir(stackFilePath)
	_, err := runCommandAndCaptureStdErr(command, args, &cmdOpts{WorkingDir: stackFolder})
	return err
}

// Remove executes the docker stack rm command.
func (service *DockerSwarmStackService) Remove(ctx context.Context, name string, filePaths []string) error {
	command := service.prepareDockerCommand(service.binaryPath)
	args := []string{"stack", "rm", name}

	_, err := runCommandAndCaptureStdErr(command, args, nil)
	return err
}

func (service *DockerSwarmStackService) prepareDockerCommand(binaryPath string) string {
	// Assume Linux as a default
	command := path.Join(binaryPath, "docker")

	if runtime.GOOS == "windows" {
		command = path.Join(binaryPath, "docker.exe")
	}

	return command
}
