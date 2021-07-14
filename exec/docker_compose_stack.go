package exec

import (
	"context"

	libstack "github.com/portainer/docker-compose-wrapper"
	"github.com/portainer/docker-compose-wrapper/composebinary"
)

// DockerComposeStackService represents a service for managing stacks by using the Docker binary.
type DockerComposeStackService struct {
	deployer   libstack.Deployer
	binaryPath string
}

// NewDockerComposeStackService initializes a new DockerStackService service.
// It also updates the configuration of the Docker CLI binary.
func NewDockerComposeStackService(binaryPath string) (*DockerComposeStackService, error) {
	deployer, err := composebinary.NewDockerComposeDeployer(binaryPath, "")
	if err != nil {
		return nil, err
	}

	service := &DockerComposeStackService{
		binaryPath: binaryPath,
		deployer:   deployer,
	}

	return service, nil
}

// Login executes the docker login command against a list of registries (including DockerHub).
func (service *DockerComposeStackService) Login() error {
	// Not implemented yet.
	return nil
}

// Logout executes the docker logout command.
func (service *DockerComposeStackService) Logout() error {
	return nil

}

// Deploy executes the docker stack deploy command.
func (service *DockerComposeStackService) Deploy(ctx context.Context, name, stackFilePath string, prune bool) error {
	return service.deployer.Deploy(ctx, "", name, []string{stackFilePath}, "")
}

// Remove executes the docker stack rm command.
func (service *DockerComposeStackService) Remove(ctx context.Context, name string) error {
	return service.deployer.Remove(ctx, "", name, []string{""})
}
