package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	ServiceNameLabel = "com.docker.stack.namespace"
)

// GetStackServices retrieves all the services associated to a stack.
func GetStackServices(ctx context.Context, stackName string) (r []swarm.Service, err error) {
	err = withCli(func(cli *client.Client) error {
		r, err = cli.ServiceList(ctx, types.ServiceListOptions{
			Filters: filters.NewArgs(filters.Arg("label", fmt.Sprintf("%s=%s", ServiceNameLabel, stackName))),
			Status:  true,
		})

		return err
	})

	return r, err
}

// GetServiceTasks retrieves all the tasks associated to a service.
func GetServiceTasks(ctx context.Context, serviceID string) (r []swarm.Task, err error) {
	err = withCli(func(cli *client.Client) error {
		r, err = cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(filters.Arg("service", serviceID)),
		})

		return err
	})

	return r, err
}
