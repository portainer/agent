package edge

import (
	"context"
	"errors"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/stack"

	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"
)

const (
	zeroDuration       = time.Duration(0)
	coalescingInterval = 100 * time.Millisecond
	failSafeInterval   = time.Minute
)

type operationError struct {
	Command   string
	Operation string
	Err       error
}

func newOperationError(cmd, op string, err error) error {
	if err == nil {
		return nil
	}

	return &operationError{
		Command:   cmd,
		Operation: op,
		Err:       err,
	}
}

func (o *operationError) Error() string {
	return o.Err.Error()
}

func (o *operationError) Is(target error) bool {
	_, ok := target.(*operationError)
	return ok
}

func createTicker(interval time.Duration) *time.Ticker {
	if interval > zeroDuration {
		return time.NewTicker(interval)
	}

	t := time.NewTicker(time.Minute)
	t.Stop()

	return t
}

func updateTicker(ticker *time.Ticker, interval time.Duration) {
	if interval <= zeroDuration {
		ticker.Stop()
		return
	}

	ticker.Reset(interval)
}

func (service *PollService) failSafe() {
	zeroPing := service.pingInterval <= zeroDuration
	zeroSnapshot := service.snapshotInterval <= zeroDuration
	zeroCommand := service.commandInterval <= zeroDuration

	if zeroPing && zeroSnapshot && zeroCommand {
		log.Warn().Msg("activating fail-safe mechanism for the async poll")

		service.pingInterval = failSafeInterval
		updateTicker(service.pingTicker, failSafeInterval)
	}
}

func (service *PollService) startStatusPollLoopAsync() {
	var pingCh, snapshotCh, commandCh <-chan time.Time

	log.Debug().Msg("starting Portainer async polling client")

	var snapshotFlag, commandFlag, coalescingFlag bool

	service.pingTicker = createTicker(service.pingInterval)
	pingCh = service.pingTicker.C

	service.snapshotTicker = createTicker(service.snapshotInterval)
	snapshotCh = service.snapshotTicker.C

	service.commandTicker = createTicker(service.commandInterval)
	commandCh = service.commandTicker.C

	service.failSafe()

	coalescingTicker := time.NewTicker(coalescingInterval)
	coalescingTicker.Stop()

	startOrKeepCoalescing := func() {
		if !coalescingFlag {
			coalescingTicker.Reset(coalescingInterval)
			coalescingFlag = true
		}
	}

	for {
		select {
		case <-pingCh:
			startOrKeepCoalescing()

		case <-snapshotCh:
			snapshotFlag = true
			startOrKeepCoalescing()

		case <-commandCh:
			commandFlag = true
			startOrKeepCoalescing()

		case <-coalescingTicker.C:
			coalescingTicker.Stop()

			log.Debug().Bool("snapshot", snapshotFlag).Bool("command", commandFlag).Msg("sending async-poll")

			err := service.pollAsync(snapshotFlag, commandFlag)
			if err != nil {
				log.Error().Err(err).Msg("an error occured during async poll")
			}

			snapshotFlag, commandFlag, coalescingFlag = false, false, false

			pingCh = service.pingTicker.C
			snapshotCh = service.snapshotTicker.C
			commandCh = service.commandTicker.C

		case <-service.startSignal:
			pingCh = service.pingTicker.C
			snapshotCh = service.snapshotTicker.C
			commandCh = service.commandTicker.C

		case <-service.stopSignal:
			log.Debug().Msg("stopping Portainer async-polling client")

			pingCh, snapshotCh, commandCh = nil, nil, nil
		}
	}
}

func (service *PollService) pollAsync(doSnapshot, doCommand bool) error {
	flags := []string{}

	if doSnapshot {
		flags = append(flags, "snapshot")
	}

	if doCommand {
		flags = append(flags, "command")
	}

	status, err := service.portainerClient.GetEnvironmentStatus(flags...)
	if err != nil {
		return err
	}

	service.processAsyncCommands(status.AsyncCommands)

	service.scheduleManager.ProcessScheduleLogsCollection()

	if status.PingInterval != service.pingInterval ||
		status.SnapshotInterval != service.snapshotInterval ||
		status.CommandInterval != service.commandInterval {

		service.pingInterval = status.PingInterval
		service.snapshotInterval = status.SnapshotInterval
		service.commandInterval = status.CommandInterval

		updateTicker(service.pingTicker, status.PingInterval)
		updateTicker(service.snapshotTicker, status.SnapshotInterval)
		updateTicker(service.commandTicker, status.CommandInterval)

		service.failSafe()
	}

	return nil
}

func (service *PollService) processAsyncCommands(commands []client.AsyncCommand) {
	ctx := context.Background()

	for _, command := range commands {
		var err error

		switch command.Type {
		case "edgeStack":
			err = service.processStackCommand(ctx, command)
		case "edgeJob":
			err = service.processScheduleCommand(command)
		case "edgeLog":
			err = service.processLogCommand(command)
		case "container":
			err = service.processContainerCommand(command)
		case "image":
			err = service.processImageCommand(command)
		case "volume":
			err = service.processVolumeCommand(command)
		default:
			err = newOperationError(command.Type, "n/a", errors.New("command type not supported"))
		}

		var opErr *operationError
		if errors.As(err, &opErr) {
			log.Error().
				Str("command", opErr.Command).
				Str("operation", opErr.Operation).
				Err(err).
				Msg("error with command operation")
		}

		service.portainerClient.SetLastCommandTimestamp(command.Timestamp)
	}
}

func (service *PollService) processStackCommand(ctx context.Context, command client.AsyncCommand) error {
	var stackData client.EdgeStackData
	err := mapstructure.Decode(command.Value, &stackData)
	if err != nil {
		return newOperationError("stack", "n/a", err)
	}

	responseStatus := int(stack.EdgeStackStatusOk)
	errorMessage := ""

	switch command.Operation {
	case "add", "replace":
		err = service.edgeStackManager.DeployStack(ctx, stackData)

		if err != nil {
			responseStatus = int(stack.EdgeStackStatusError)
			errorMessage = err.Error()
		}

	case "remove":
		responseStatus = int(stack.EdgeStackStatusRemove)

		err = service.edgeStackManager.DeleteStack(ctx, stackData)
		if err != nil {
			responseStatus = int(stack.EdgeStackStatusError)
			errorMessage = err.Error()
		}

	default:
		return newOperationError("schedule", command.Operation, errors.New("operation not supported"))
	}

	return service.portainerClient.SetEdgeStackStatus(stackData.ID, responseStatus, errorMessage)
}

func (service *PollService) processScheduleCommand(command client.AsyncCommand) error {
	var jobData client.EdgeJobData
	err := mapstructure.Decode(command.Value, &jobData)
	if err != nil {
		return newOperationError("schedule", "n/a", err)
	}

	schedule := agent.Schedule{
		ID:             int(jobData.ID),
		CronExpression: jobData.CronExpression,
		Script:         jobData.ScriptFileContent,
		Version:        jobData.Version,
		CollectLogs:    jobData.CollectLogs,
	}

	switch command.Operation {
	case "add", "replace":
		err = service.scheduleManager.AddSchedule(schedule)

	case "remove":
		err = service.scheduleManager.RemoveSchedule(schedule)

	default:
		err = errors.New("operation not supported")
	}

	return newOperationError("schedule", command.Operation, err)
}

func (service *PollService) processLogCommand(command client.AsyncCommand) error {
	var logCmd client.LogCommandData

	err := mapstructure.Decode(command.Value, &logCmd)
	if err != nil {
		return newOperationError("log", "n/a", err)
	}

	service.portainerClient.EnqueueLogCollectionForStack(logCmd)

	return nil
}

func (service *PollService) processContainerCommand(command client.AsyncCommand) error {
	var containerCmd client.ContainerCommandData

	err := mapstructure.Decode(command.Value, &containerCmd)
	if err != nil {
		return newOperationError("container", "n/a", err)
	}

	switch containerCmd.ContainerOperation {
	case "start":
		err = docker.ContainerStart(containerCmd.ContainerName, containerCmd.ContainerStartOptions)
	case "restart":
		err = docker.ContainerRestart(containerCmd.ContainerName)
	case "stop":
		err = docker.ContainerStop(containerCmd.ContainerName)
	case "delete":
		err = docker.ContainerDelete(containerCmd.ContainerName, containerCmd.ContainerRemoveOptions)
	case "kill":
		err = docker.ContainerKill(containerCmd.ContainerName)
	}

	return newOperationError("container", command.Operation, err)
}

func (service *PollService) processImageCommand(command client.AsyncCommand) error {
	var imageCommand client.ImageCommandData

	err := mapstructure.Decode(command.Value, &imageCommand)
	if err != nil {
		return newOperationError("image", "n/a", errors.New("failed to decode ImageCommandData"))
	}

	switch imageCommand.ImageOperation {
	case "delete":
		_, err = docker.ImageDelete(imageCommand.ImageName, imageCommand.ImageRemoveOptions)
	}

	return newOperationError("image", command.Operation, err)
}

func (service *PollService) processVolumeCommand(command client.AsyncCommand) error {
	var volumeCommand client.VolumeCommandData

	err := mapstructure.Decode(command.Value, &volumeCommand)
	if err != nil {
		return newOperationError("volume", "n/a", err)
	}

	switch volumeCommand.VolumeOperation {
	case "delete":
		err = docker.VolumeDelete(volumeCommand.VolumeName, volumeCommand.ForceRemove)
	}

	return newOperationError("volume", command.Operation, err)
}
