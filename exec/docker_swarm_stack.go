package exec

import (
	"context"

	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	libstack "github.com/portainer/portainer/pkg/libstack"
	libswarm "github.com/portainer/portainer/pkg/libstack/swarm"
)

var _ deployer.Deployer = &DockerSwarmStackService{}

// DockerSwarmStackService manages Docker Swarm stacks via the SwarmDeployer.
type DockerSwarmStackService struct {
	swarmDeployer libswarm.Deployer
}

// NewDockerSwarmStackService initializes a new DockerSwarmStackService.
func NewDockerSwarmStackService() *DockerSwarmStackService {
	return &DockerSwarmStackService{
		swarmDeployer: libswarm.NewSwarmDeployer(),
	}
}

// Deploy creates or updates a Docker Swarm stack from the given compose files.
func (service *DockerSwarmStackService) Deploy(
	ctx context.Context,
	name string,
	filePaths []string,
	options deployer.DeployOptions,
) error {
	return service.swarmDeployer.Deploy(ctx, filePaths, libswarm.DeployOptions{
		Options: libswarm.Options{
			ProjectName: name,
			WorkingDir:  options.WorkingDir,
			Env:         options.Env,
			Registries:  registryCredsToAuthConfigs(options.Registries),
		},
		RemoveOrphans: options.Prune,
		PullImage:     true,
	})
}

// Pull is a no-op for Swarm; images are pulled on deploy.
func (service *DockerSwarmStackService) Pull(_ context.Context, _ string, _ []string, _ deployer.PullOptions) error {
	return nil
}

// Validate checks that the compose file(s) are valid for swarm deployment.
func (service *DockerSwarmStackService) Validate(ctx context.Context, name string, filePaths []string, options deployer.ValidateOptions) error {
	return service.swarmDeployer.Validate(ctx, filePaths, libswarm.Options{
		ProjectName: name,
		WorkingDir:  options.WorkingDir,
		Env:         options.Env,
		Registries:  registryCredsToAuthConfigs(options.Registries),
	})
}

// Remove deletes all resources belonging to a Swarm stack.
func (service *DockerSwarmStackService) Remove(ctx context.Context, name string, _ []string, options deployer.RemoveOptions) error {
	return service.swarmDeployer.Remove(ctx, name, libswarm.RemoveOptions{
		Options: libswarm.Options{
			ProjectName: name,
			Env:         options.Env,
		},
	})
}

func (service *DockerSwarmStackService) WaitForStatus(ctx context.Context, name string, status libstack.Status, _ deployer.CheckStatusOptions) libstack.WaitResult {
	return service.swarmDeployer.WaitForStatus(ctx, name, libswarm.Options{}, status)
}

func (service *DockerSwarmStackService) GetEdgeStacks(_ context.Context) ([]agent.EdgeStack, error) {
	return nil, nil
}
