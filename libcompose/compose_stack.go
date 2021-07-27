package libcompose

import (
	"context"

	"github.com/portainer/libcompose/docker"
	"github.com/portainer/libcompose/docker/ctx"
	"github.com/portainer/libcompose/project"
	"github.com/portainer/libcompose/project/options"
)

const (
	dockerClientVersion     = "1.24"
	composeSyntaxMaxVersion = "2"
)

// DockerComposeStackService represents a service for managing compose stacks.
type DockerComposeStackService struct {
}

// NewDockerComposeStackService initializes a new DockerComposeStackService service.
func NewDockerComposeStackService() *DockerComposeStackService {
	return &DockerComposeStackService{}
}

func (manager *DockerComposeStackService) Login() error {
	// Not implemented yet.
	return nil
}

func (manager *DockerComposeStackService) Logout() error {
	// Not implemented yet.
	return nil
}

func (manager *DockerComposeStackService) Deploy(name, stackFilePath string, prune bool) error {

	proj, err := docker.NewProject(&ctx.Context{
		Context: project.Context{
			ComposeFiles: []string{stackFilePath},

			ProjectName: name,
		},
	}, nil)
	if err != nil {
		return err
	}

	return proj.Up(context.Background(), options.Up{})
}

func (manager *DockerComposeStackService) Remove(name string) error {
	proj, err := docker.NewProject(&ctx.Context{
		Context: project.Context{
			ProjectName: name,
		},
	}, nil)
	if err != nil {
		return err
	}

	return proj.Down(context.Background(), options.Down{RemoveVolume: false, RemoveOrphans: true})
}
