package exec

import (
	"context"
	"errors"
	"path"
	"runtime"

	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	libstack "github.com/portainer/portainer/pkg/libstack"
	"github.com/portainer/portainer/pkg/libstack/compose"
)

var _ deployer.Deployer = &DockerSwarmStackService{}

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
func NewDockerSwarmStackService(binaryPath string) *DockerSwarmStackService {
	command := path.Join(binaryPath, "docker")

	if runtime.GOOS == "windows" {
		command = path.Join(binaryPath, "docker.exe")
	}

	return &DockerSwarmStackService{
		command:         command,
		composeDeployer: compose.NewComposeDeployer(),
	}
}

// Deploy executes the docker stack deploy command.
func (service *DockerSwarmStackService) Deploy(ctx context.Context, name string, filePaths []string, options deployer.DeployOptions) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args := []string{"stack", "deploy", "--with-registry-auth"}
	if options.Prune {
		args = append(args, "--prune")
	}
	args = append(args, "--compose-file", stackFilePath, name)

	stackFolder := options.WorkingDir
	if stackFolder == "" {
		stackFolder = path.Dir(stackFilePath)
	}

	_, err := runCommandAndCaptureStdErr(service.command, args, &cmdOpts{
		WorkingDir:  stackFolder,
		Env:         options.Env,
		ProjectName: name,
	})

	return err
}

// Pull is a dummy method for Swarm
func (service *DockerSwarmStackService) Pull(ctx context.Context, name string, filePaths []string, options deployer.PullOptions) error {
	return nil
}

// Validate uses compose to validate the stack files
func (service *DockerSwarmStackService) Validate(ctx context.Context, name string, filePaths []string, options deployer.ValidateOptions) error {
	return service.composeDeployer.Validate(ctx, filePaths, libstack.Options{
		WorkingDir:  options.WorkingDir,
		Env:         options.Env,
		ProjectName: name,
	})
}

// Remove executes the docker stack rm command.
func (service *DockerSwarmStackService) Remove(ctx context.Context, name string, filePaths []string, options deployer.RemoveOptions) error {
	args := []string{"stack", "rm", name}

	_, err := runCommandAndCaptureStdErr(service.command, args, &cmdOpts{
		Env:         options.Env,
		ProjectName: name,
	})

	return err
}

func (service *DockerSwarmStackService) GetEdgeStacks(ctx context.Context) ([]agent.EdgeStack, error) {
	return nil, nil
}
