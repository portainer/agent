package exec

import (
	"context"
	"strings"

	"github.com/docker/cli/cli/config/types"
	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	"github.com/portainer/portainer/api/edge"
	libstack "github.com/portainer/portainer/pkg/libstack"
	"github.com/portainer/portainer/pkg/libstack/compose"
)

var _ deployer.Deployer = &DockerComposeStackService{}

// DockerComposeStackService represents a service for managing stacks by using the Docker binary.
type DockerComposeStackService struct {
	deployer libstack.Deployer
}

// NewDockerComposeStackService initializes a new DockerStackService service.
// It also updates the configuration of the Docker CLI binary.
func NewDockerComposeStackService() *DockerComposeStackService {
	return &DockerComposeStackService{
		deployer: compose.NewComposeDeployer(),
	}
}

// Deploy executes the docker stack deploy command.
func (service *DockerComposeStackService) Deploy(ctx context.Context, name string, filePaths []string, options deployer.DeployOptions) error {
	return service.deployer.Deploy(ctx, filePaths, libstack.DeployOptions{
		Options: libstack.Options{
			ProjectName: name,
			WorkingDir:  options.WorkingDir,
			Env:         options.Env,
			Registries:  registryCredsToAuthConfigs(options.Registries),
		},
		ForceRecreate: options.ForceRecreate,
		RemoveOrphans: options.Prune,
		EdgeStackID:   options.EdgeStackID,
	})
}

// Pull executes the docker pull command.
func (service *DockerComposeStackService) Pull(ctx context.Context, name string, filePaths []string, options deployer.PullOptions) error {
	return service.deployer.Pull(ctx, filePaths, libstack.Options{
		ProjectName: name,
		WorkingDir:  options.WorkingDir,
		Env:         options.Env,
		Registries:  registryCredsToAuthConfigs(options.Registries),
	})
}

// Remove executes the docker stack rm command.
func (service *DockerComposeStackService) Remove(ctx context.Context, name string, filePaths []string, options deployer.RemoveOptions) error {
	return service.deployer.Remove(ctx, name, filePaths, libstack.RemoveOptions{
		Options: libstack.Options{
			ProjectName: name,
			Env:         options.Env,
		},
		Volumes: options.Volumes,
	})
}

// Validate executes docker config command to validate file format
func (service *DockerComposeStackService) Validate(ctx context.Context, name string, filePaths []string, options deployer.ValidateOptions) error {
	return service.deployer.Validate(ctx, filePaths, libstack.Options{
		ProjectName: name,
		WorkingDir:  options.WorkingDir,
		Env:         options.Env,
	})
}

func (service *DockerComposeStackService) WaitForStatus(ctx context.Context, name string, status libstack.Status, _ deployer.CheckStatusOptions) libstack.WaitResult {
	return service.deployer.WaitForStatus(ctx, name, status)
}

func (service *DockerComposeStackService) GetEdgeStacks(ctx context.Context) ([]agent.EdgeStack, error) {
	var r []agent.EdgeStack

	edgeStacks, err := service.deployer.GetExistingEdgeStacks(ctx)
	if err != nil {
		return nil, err
	}

	for _, s := range edgeStacks {
		// Remove the prefix because it will get added back by the stack manager
		s.Name = strings.TrimPrefix(s.Name, "edge_")

		r = append(r, agent.EdgeStack(s))
	}

	return r, nil
}

func registryCredsToAuthConfigs(registryCreds []edge.RegistryCredentials) []types.AuthConfig {
	var authConfigs []types.AuthConfig

	for _, r := range registryCreds {
		authConfigs = append(authConfigs, types.AuthConfig{
			Username:      r.Username,
			Password:      r.Secret,
			ServerAddress: r.ServerURL,
		})
	}

	return authConfigs
}
