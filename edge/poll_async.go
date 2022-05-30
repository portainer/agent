package edge

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/stack"
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
		log.Println("[WARN] [edge] [async] [message: activating fail-safe mechanism for the async poll]")
		service.pingInterval = failSafeInterval
		updateTicker(service.pingTicker, failSafeInterval)
	}
}

func (service *PollService) startStatusPollLoopAsync() {
	var pingCh, snapshotCh, commandCh <-chan time.Time

	log.Println("[DEBUG] [edge] [message: starting Portainer async polling client]")

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

			log.Printf("[DEBUG] [edge] [async] [snapshot: %v] [command: %v] [message: sending async-poll]", snapshotFlag, commandFlag)

			err := service.pollAsync(snapshotFlag, commandFlag)
			if err != nil {
				log.Printf("[ERROR] [edge] [message: an error occured during async poll] [error: %s]", err)
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
			log.Println("[DEBUG] [edge] [async] [message: stopping Portainer async-polling client]")

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

	err = service.processAsyncCommands(status.AsyncCommands)
	if err != nil {
		return err
	}

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
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to EdgeStackData", command.Value)
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
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to EdgeJobData", command.Value)
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
		if err != nil {
			log.Printf("[ERROR] [edge] [message: error adding schedule] [error: %s]", err)
		}

	case "remove":
		err = service.scheduleManager.RemoveSchedule(schedule)
		if err != nil {
			log.Printf("[ERROR] [edge] [message: error removing schedule] [error: %s]", err)
		}

	default:
		return fmt.Errorf("operation %v not supported", command.Operation)
	}

	return nil
}

func (service *PollService) processLogCommand(command client.AsyncCommand) error {
	var logCmd client.LogCommandData

	err := mapstructure.Decode(command.Value, &logCmd)
	if err != nil {
		log.Printf("[DEBUG] [http,client,portainer] failed to convert %v to LogCommandData", command.Value)
		return err
	}

	service.portainerClient.EnqueueLogCollectionForStack(logCmd)

	return nil
}
