package exec

import (
	wrapper "github.com/portainer/docker-compose-wrapper"
)

// DockerComposeStackService represents a service for managing stacks by using the Docker binary.
type DockerComposeStackService struct {
	wrapper    *wrapper.ComposeWrapper
	binaryPath string
}

// NewDockerComposeStackService initializes a new DockerStackService service.
// It also updates the configuration of the Docker CLI binary.
func NewDockerComposeStackService(binaryPath string) (*DockerComposeStackService, error) {
	wrap, err := wrapper.NewComposeWrapper(binaryPath)
	if err != nil {
		return nil, err
	}

	service := &DockerComposeStackService{
		binaryPath: binaryPath,
		wrapper:    wrap,
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
func (service *DockerComposeStackService) Deploy(name, stackFilePath string, prune bool) error {
	_, err := service.wrapper.Up(stackFilePath, "", name, "")
	return err
}

// Remove executes the docker stack rm command.
func (service *DockerComposeStackService) Remove(name string) error {
	_, err := service.wrapper.Down("", "", name)
	return err
}
