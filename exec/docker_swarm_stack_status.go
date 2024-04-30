package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/portainer/agent/docker"
	libstack "github.com/portainer/portainer/pkg/libstack"
	"github.com/rs/zerolog/log"
)

func (service *DockerSwarmStackService) WaitForStatus(ctx context.Context, name string, status libstack.Status) <-chan libstack.WaitResult {
	waitResultCh := make(chan libstack.WaitResult)
	waitResult := libstack.WaitResult{
		Status: status,
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				waitResult.ErrorMsg = fmt.Sprintf("failed to wait for status: %s", ctx.Err().Error())
				waitResultCh <- waitResult
			default:
			}

			time.Sleep(1 * time.Second)

			cli, err := docker.NewClient()
			if err != nil {
				log.Warn().Err(err).Msg("failed to create Docker client")
				continue
			}

			// Create a filter to match the services belonging to the stack
			stackFilter := filters.NewArgs()
			stackFilter.Add("label", fmt.Sprintf("com.docker.stack.namespace=%s", name))

			// Retrieve the services of the stack
			services, err := cli.ServiceList(ctx, types.ServiceListOptions{
				Filters: stackFilter,
			})
			if err != nil {
				log.Warn().
					Str("project_name", name).
					Err(err).
					Msg("failed to list Docker services")
			}

			if len(services) == 0 && status == libstack.StatusRemoved {
				waitResultCh <- waitResult
				return
			}
			var serviceStatuses []libstack.Status
			for _, service := range services {
				serviceStatus, statusMessage, err := getServiceStatus(ctx, cli, service)
				if err != nil {
					log.Warn().
						Str("project_name", name).
						Err(err).
						Msg("failed to get service status")
					continue
				}

				if statusMessage != "" {
					waitResult.ErrorMsg = statusMessage
					waitResultCh <- waitResult
					return
				}

				serviceStatuses = append(serviceStatuses, serviceStatus)
			}

			// Aggregate the statuses of all services
			aggregateStatus := aggregateStatus(serviceStatuses)

			if aggregateStatus == status {
				waitResultCh <- waitResult
				return
			}

			log.Debug().
				Str("project_name", name).
				Str("status", string(aggregateStatus)).
				Msg("waiting for status")
		}
	}()

	return waitResultCh
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

	for _, status := range statuses {
		if status == libstack.StatusUnknown {
			return libstack.StatusUnknown
		}
	}

	// If all services are running, return "running"
	return libstack.StatusRunning
}

func getServiceStatus(ctx context.Context, cli *client.Client, service swarm.Service) (libstack.Status, string, error) {
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

	expectedRunningTaskCount := 0
	if service.Spec.Mode.Replicated != nil {
		expectedRunningTaskCount = int(*service.Spec.Mode.Replicated.Replicas)
	}

	if service.Spec.Mode.Global != nil {
		nodes, err := cli.NodeList(ctx, types.NodeListOptions{})
		if err != nil {
			return "", "", fmt.Errorf("failed to list nodes: %v", err)
		}

		expectedRunningTaskCount = len(nodes)
	}

	if expectedRunningTaskCount != 0 {
		runningTaskCount := 0
		for _, task := range tasks {
			if task.Status.State == swarm.TaskStateRunning {
				runningTaskCount++
			}
		}

		if runningTaskCount == expectedRunningTaskCount {
			return libstack.StatusRunning, "", nil
		}
	}

	// Check the status of each task and append it to the serviceStatuses slice
	for _, task := range tasks {
		switch task.Status.State {
		case swarm.TaskStateRunning:
			return libstack.StatusRunning, "", nil
		case swarm.TaskStatePending, swarm.TaskStateStarting:
			// case swarm.Task
			return libstack.StatusStarting, "", nil
		case swarm.TaskStateRemove:
			return libstack.StatusRemoving, "", nil
		case swarm.TaskStateFailed:
			return libstack.StatusError, task.Status.Err, nil
		default:
			return libstack.StatusUnknown, "", nil
		}
	}

	return "", "", nil
}
