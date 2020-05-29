package exec

import (
	"bytes"
	"errors"
	"os/exec"
	"path"
	"runtime"
)

// DockerStackService represents a service for managing stacks by using the Docker binary.
type DockerStackService struct {
	binaryPath string
}

// NewDockerStackService initializes a new DockerStackService service.
// It also updates the configuration of the Docker CLI binary.
func NewDockerStackService(binaryPath string) (*DockerStackService, error) {
	service := &DockerStackService{
		binaryPath: binaryPath,
	}

	return service, nil
}

// Login executes the docker login command against a list of registries (including DockerHub).
func (service *DockerStackService) Login() error {
	// Not implemented yet.
	return nil
}

// Logout executes the docker logout command.
func (service *DockerStackService) Logout() error {
	command := service.prepareDockerCommand(service.binaryPath)
	args := []string{"logout"}
	return runCommandAndCaptureStdErr(command, args, "")

}

// Deploy executes the docker stack deploy command.
func (service *DockerStackService) Deploy(name, stackFilePath string, prune bool) error {
	command := service.prepareDockerCommand(service.binaryPath)

	args := []string{}
	if prune {
		args = append(args, "stack", "deploy", "--prune", "--with-registry-auth", "--compose-file", stackFilePath, name)
	} else {
		args = append(args, "stack", "deploy", "--with-registry-auth", "--compose-file", stackFilePath, name)
	}

	stackFolder := path.Dir(stackFilePath)
	return runCommandAndCaptureStdErr(command, args, stackFolder)
}

// Remove executes the docker stack rm command.
func (service *DockerStackService) Remove(name string) error {
	command := service.prepareDockerCommand(service.binaryPath)
	args := []string{"stack", "rm", name}
	return runCommandAndCaptureStdErr(command, args, "")
}

func runCommandAndCaptureStdErr(command string, args []string, workingDir string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(command, args...)
	cmd.Stderr = &stderr
	cmd.Dir = workingDir

	err := cmd.Run()
	if err != nil {
		return errors.New(stderr.String())
	}

	return nil
}

func (service *DockerStackService) prepareDockerCommand(binaryPath string) string {
	// Assume Linux as a default
	command := path.Join(binaryPath, "docker")

	if runtime.GOOS == "windows" {
		command = path.Join(binaryPath, "docker.exe")
	}

	return command
}
