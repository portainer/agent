package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/portainer/agent"
)

const (
	ServiceNameLabel = "com.docker.stack.namespace"
)

// GetStackServices retrieves all the services associated to a stack.
func GetStackServices(ctx context.Context, stackName string) ([]swarm.Service, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	services, err := cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", fmt.Sprintf("%s=%s", ServiceNameLabel, stackName))),
		Status:  true,
	})

	if err != nil {
		return nil, err
	}

	return services, nil
}

// GetServiceTasks retrieves all the tasks associated to a service.
func GetServiceTasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}

	defer cli.Close()

	return cli.TaskList(ctx, types.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("service", serviceID)),
	})
}
