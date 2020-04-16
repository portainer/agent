package exec

import (
	"bytes"
	"errors"
	"os/exec"
	"path"
	"runtime"
)

// EdgeStackManager represents a service for managing stacks.
type EdgeStackManager struct {
	binaryPath string
}

// NewEdgeStackManager initializes a new EdgeStackManager service.
// It also updates the configuration of the Docker CLI binary.
func NewEdgeStackManager(binaryPath string) (*EdgeStackManager, error) {
	manager := &EdgeStackManager{
		binaryPath: binaryPath,
	}

	return manager, nil
}

// Login executes the docker login command against a list of registries (including DockerHub).
func (manager *EdgeStackManager) Login() error {
	// Not implemented yet.
	return nil
}

// Logout executes the docker logout command.
func (manager *EdgeStackManager) Logout() error {
	command := manager.prepareDockerCommand(manager.binaryPath)
	args := []string{"logout"}
	return runCommandAndCaptureStdErr(command, args, "")

}

// Deploy executes the docker stack deploy command.
func (manager *EdgeStackManager) Deploy(name, projectPath, entryPoint string, prune bool) error {
	stackFilePath := path.Join(projectPath, entryPoint)
	command := manager.prepareDockerCommand(manager.binaryPath)

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
func (manager *EdgeStackManager) Remove(name string) error {
	command := manager.prepareDockerCommand(manager.binaryPath)
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

func (manager *EdgeStackManager) prepareDockerCommand(binaryPath string) string {
	// Assume Linux as a default
	command := path.Join(binaryPath, "docker")

	if runtime.GOOS == "windows" {
		command = path.Join(binaryPath, "docker.exe")
	}

	return command
}
