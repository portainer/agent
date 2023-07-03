package exec

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/portainer/agent/docker"
	libstack "github.com/portainer/portainer/pkg/libstack"
)

func GetStackStatus(ctx context.Context, stackName string) (libstack.Status, string, error) {
	cli, err := docker.NewClient()
	if err != nil {
		return "", "", fmt.Errorf("failed to create Docker client: %v", err)
	}

	// Create a filter to match the services belonging to the stack
	stackFilter := filters.NewArgs()
	stackFilter.Add("label", fmt.Sprintf("com.docker.stack.namespace=%s", stackName))

	// Retrieve the services of the stack
	services, err := cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: stackFilter,
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to list Docker services: %v", err)
	}

	var serviceStatuses []libstack.Status
	for _, service := range services {
		// Retrieve the tasks for each service
		tasks, err := cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(filters.KeyValuePair{
				Key:   "service",
				Value: service.ID,
			}),
		})
		if err != nil {
			return "", "", fmt.Errorf("failed to list tasks for service %s: %v", service.Spec.Name, err)
		}

		// Check the status of each task and append it to the serviceStatuses slice
		for _, task := range tasks {
			switch task.Status.State {
			case swarm.TaskStateRunning:
				serviceStatuses = append(serviceStatuses, libstack.StatusRunning)
			case swarm.TaskStatePending:
			case swarm.TaskStateStarting:
				// case swarm.Task
				serviceStatuses = append(serviceStatuses, libstack.StatusStarting)
			case swarm.TaskStateRemove:
				serviceStatuses = append(serviceStatuses, libstack.StatusRemoving)
			case swarm.TaskStateFailed:
				return libstack.StatusError, task.Status.Err, nil
			default:
				serviceStatuses = append(serviceStatuses, libstack.StatusUnknown)
			}
		}
	}

	// Aggregate the statuses of all services
	status := aggregateStatus(serviceStatuses)

	return status, "", nil
}

func aggregateStatus(statuses []libstack.Status) libstack.Status {
	// Determine the overall status based on the individual service statuses
	if len(statuses) == 0 {
		return libstack.StatusRemoved
	}

	// If any service has failed, return "failed"
	for _, status := range statuses {
		if status == libstack.StatusError {
			return libstack.StatusError
		}
	}

	// If any service is pending, return "pending"
	for _, status := range statuses {
		if status == libstack.StatusStarting {
			return libstack.StatusStarting
		}
	}

	// If any service is removing, return "removing"
	for _, status := range statuses {
		if status == libstack.StatusRemoving {
			return libstack.StatusRemoving
		}
	}

	// If any service is starting, return "starting"
	for _, status := range statuses {
		if status == libstack.StatusStarting {
			return libstack.StatusStarting
		}
	}

	// If all services are running, return "running"
	return libstack.StatusRunning
}
