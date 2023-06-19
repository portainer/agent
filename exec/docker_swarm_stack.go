package exec

import (
	"context"
	"errors"
	"path"
	"runtime"

	"github.com/portainer/agent"
	libstack "github.com/portainer/portainer/pkg/libstack"
	"github.com/portainer/portainer/pkg/libstack/compose"
	"github.com/rs/zerolog/log"
)

// DockerSwarmStackService represents a service for managing stacks by using the Docker binary.
type DockerSwarmStackService struct {
	command         string
	composeDeployer libstack.Deployer
}

type DockerSwarmDeployOpts struct {
	Prune bool
}

// NewDockerSwarmStackService initializes a new DockerStackService service.
// It also updates the configuration of the Docker CLI binary.
func NewDockerSwarmStackService(binaryPath string) (*DockerSwarmStackService, error) {
	// Assume Linux as a default
	command := path.Join(binaryPath, "docker")

	if runtime.GOOS == "windows" {
		command = path.Join(binaryPath, "docker.exe")
	}

	composeDeployer, err := compose.NewComposeDeployer(binaryPath, "")
	if err != nil {
		return nil, err
	}

	service := &DockerSwarmStackService{
		command:         command,
		composeDeployer: composeDeployer,
	}

	return service, nil
}

// Deploy executes the docker stack deploy command.
func (service *DockerSwarmStackService) Deploy(ctx context.Context, name string, filePaths []string, options agent.DeployOptions) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args := []string{}
	if options.Prune {
		args = append(args, "stack", "deploy", "--prune", "--with-registry-auth", "--compose-file", stackFilePath, name)
	} else {
		args = append(args, "stack", "deploy", "--with-registry-auth", "--compose-file", stackFilePath, name)
	}

	stackFolder := options.WorkingDir
	if stackFolder == "" {
		stackFolder = path.Dir(stackFilePath)
	}
	_, err := runCommandAndCaptureStdErr(service.command, args, &cmdOpts{
		WorkingDir: stackFolder,
		Env:        options.Env,
	})
	return err
}

// Pull is a dummy method for Swarm
func (service *DockerSwarmStackService) Pull(ctx context.Context, name string, filePaths []string, options agent.PullOptions) error {
	return nil
}

// Validate uses compose to validate the stack files
func (service *DockerSwarmStackService) Validate(ctx context.Context, name string, filePaths []string, options agent.ValidateOptions) error {
	return service.composeDeployer.Validate(ctx, filePaths, libstack.Options{
		WorkingDir: options.WorkingDir,
		Env:        options.Env,
	})
}

// Remove executes the docker stack rm command.
func (service *DockerSwarmStackService) Remove(ctx context.Context, name string, filePaths []string, options agent.RemoveOptions) error {
	args := []string{"stack", "rm", name}

	_, err := runCommandAndCaptureStdErr(service.command, args, &cmdOpts{
		WorkingDir: options.WorkingDir,
		Env:        options.Env,
	})
	return err
}

// Status returns the status of a stack.
func (service *DockerSwarmStackService) Status(ctx context.Context, name string) (libstack.Status, string, error) {
	log.Warn().Msg("DockerSwarmStackService.Status is not implemented")

	return "", "", nil
}
