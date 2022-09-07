package edge

import (
	"context"
	"fmt"
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

	snapshotFlag := true
	commandFlag := true
	coalescingFlag := false

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

	ping := func() {

		log.Debug().Bool("snapshot", snapshotFlag).Bool("command", commandFlag).Msg("sending async-poll")

		err := service.pollAsync(snapshotFlag, commandFlag)
		if err != nil {
			log.Error().Err(err).Msg("an error occured during async poll")
		}

		snapshotFlag, commandFlag, coalescingFlag = false, false, false

		pingCh = service.pingTicker.C
		snapshotCh = service.snapshotTicker.C
		commandCh = service.commandTicker.C
	}

	ping()

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
			ping()

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
	log.Printf("[DEBUG] [edge] [async] [message: polling Portainer] [snapshot: %t] [command: %t]", doSnapshot, doCommand)
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

	service.processUpdate(status.VersionUpdate)

	err = service.processAsyncCommands(status.AsyncCommands)
	if err != nil {
		return err
	}

	service.scheduleManager.ProcessScheduleLogsCollection()

	if status.PingInterval != service.pingInterval ||
		status.SnapshotInterval != service.snapshotInterval ||
		status.CommandInterval != service.commandInterval {

		log.Printf("[DEBUG] [edge] [async] [message: updating polling intervals] [ping: %s] [snapshot: %s] [command: %s]", status.PingInterval, status.SnapshotInterval, status.CommandInterval)

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

func (service *PollService) processAsyncCommands(commands []client.AsyncCommand) error {
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
			return fmt.Errorf("command type %s not supported", command.Type)
		}

		if err != nil {
			return err
		}

		service.portainerClient.SetLastCommandTimestamp(command.Timestamp)
	}

	return nil
}

func (service *PollService) processStackCommand(ctx context.Context, command client.AsyncCommand) error {
	var stackData client.EdgeStackData
	err := mapstructure.Decode(command.Value, &stackData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to decode EdgeStackData")

		return err
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
		return fmt.Errorf("operation %v not supported", command.Operation)
	}

	return service.portainerClient.SetEdgeStackStatus(stackData.ID, responseStatus, errorMessage)
}

func (service *PollService) processScheduleCommand(command client.AsyncCommand) error {
	var jobData client.EdgeJobData
	err := mapstructure.Decode(command.Value, &jobData)
	if err != nil {
		log.Debug().Err(err).Msg("failed to decode EdgeJobData")

		return err
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
		return fmt.Errorf("operation %v not supported", command.Operation)
	}

	if err != nil {
		log.Error().Str("operation", command.Operation).Err(err).Msg("error with operation on schedule")
	}

	return nil
}

func (service *PollService) processLogCommand(command client.AsyncCommand) error {
	var logCmd client.LogCommandData

	err := mapstructure.Decode(command.Value, &logCmd)
	if err != nil {
		log.Debug().Err(err).Msg("failed to decode LogCommandData")

		return err
	}

	service.portainerClient.EnqueueLogCollectionForStack(logCmd)

	return nil
}

func (service *PollService) processContainerCommand(command client.AsyncCommand) error {
	var containerCmd client.ContainerCommandData

	err := mapstructure.Decode(command.Value, &containerCmd)
	if err != nil {
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to ContainerCommandData: %s", command.Value, err)
		return err
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

	if err != nil {
		log.Printf("[ERROR] [edge] [message: error with '%s' operation on container command] [error: %s]", command.Operation, err)
	}

	return err
}

func (service *PollService) processImageCommand(command client.AsyncCommand) error {
	var imageCommand client.ImageCommandData

	err := mapstructure.Decode(command.Value, &imageCommand)
	if err != nil {
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to ImageCommandData: %s", command.Value, err)
		return err
	}

	switch imageCommand.ImageOperation {
	case "delete":
		_, err = docker.ImageDelete(imageCommand.ImageName, imageCommand.ImageRemoveOptions)
	}

	if err != nil {
		log.Printf("[ERROR] [edge] [message: error with '%s' operation on image command] [error: %s]", command.Operation, err)
	}

	return err
}

func (service *PollService) processVolumeCommand(command client.AsyncCommand) error {
	var volumeCommand client.VolumeCommandData

	err := mapstructure.Decode(command.Value, &volumeCommand)
	if err != nil {
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to VolumeCommandData: %s", command.Value, err)
		return err
	}

	switch volumeCommand.VolumeOperation {
	case "delete":
		err = docker.VolumeDelete(volumeCommand.VolumeName, volumeCommand.ForceRemove)
	}

	if err != nil {
		log.Printf("[ERROR] [edge] [message: error with '%s' operation on volume command] [error: %s]", command.Operation, err)
	}

	return err
}
