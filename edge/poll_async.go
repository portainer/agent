package edge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/edge/client"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"

	"github.com/go-viper/mapstructure/v2"
	"github.com/rs/zerolog/log"
	segmentjson "github.com/segmentio/encoding/json"
)

const (
	zeroDuration       = time.Duration(0)
	coalescingInterval = 100 * time.Millisecond
	failSafeInterval   = time.Minute

	EdgeAsyncCommandTypeConfig      EdgeAsyncCommandType = "edgeConfig"
	EdgeAsyncCommandTypeStack       EdgeAsyncCommandType = "edgeStack"
	EdgeAsyncCommandTypeJob         EdgeAsyncCommandType = "edgeJob"
	EdgeAsyncCommandTypeLog         EdgeAsyncCommandType = "edgeLog"
	EdgeAsyncCommandTypeContainer   EdgeAsyncCommandType = "container"
	EdgeAsyncCommandTypeImage       EdgeAsyncCommandType = "image"
	EdgeAsyncCommandTypeVolume      EdgeAsyncCommandType = "volume"
	EdgeAsyncCommandTypeNormalStack EdgeAsyncCommandType = "normalStack"

	EdgeAsyncCommandOpAdd     EdgeAsyncCommandOperation = "add"
	EdgeAsyncCommandOpRemove  EdgeAsyncCommandOperation = "remove"
	EdgeAsyncCommandOpReplace EdgeAsyncCommandOperation = "replace"
)

type (
	EdgeAsyncCommandType      string
	EdgeAsyncCommandOperation string
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

			if err := service.pollAsync(snapshotFlag, commandFlag); err != nil {
				log.Error().Err(err).Msg("an error occurred during async poll")
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

func asyncPollFlags(doSnapshot, doCommand bool) []string {
	flags := []string{}

	if doSnapshot {
		flags = append(flags, "snapshot")
	}

	if doCommand {
		flags = append(flags, "command")
	}

	return flags
}

func (service *PollService) pollAsync(doSnapshot, doCommand bool) error {
	if service.edgeID == "" {
		return errors.New("edge ID is not set")
	}

	flags := asyncPollFlags(doSnapshot, doCommand)

	if service.firstPoll {
		ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
		defer cancelFn()

		if err := service.edgeStackManager.LoadExistingPortainerUpdaterEdgeStack(ctx); err == nil {
			service.firstPoll = false
		} else {
			log.Warn().Err(err).Msg("failed to load existing portainer updater edge stack")
		}
	}

	status, err := service.portainerClient.GetEnvironmentStatus(flags...)
	if err != nil {
		var nonOkError *client.NonOkResponseError
		if errors.As(err, &nonOkError) {
			service.edgeManager.SetEndpointID(globalKeyInUse)
			service.edgeStackManager.ResetStacks()
			service.firstPoll = false

			service.policies = nil
			service.policyManager.Reset()
			service.reconciler.Reset()

			if ac, ok := service.portainerClient.(*client.PortainerAsyncClient); ok {
				ac.RequestResync()
			}
		}

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
			err = service.processEdgeStackCommand(ctx, command)
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
		case "normalStack":
			err = service.processNormalStackCommand(ctx, command)
		case "edgeConfig":
			err = service.processEdgeConfigCommand(command)
		case "policyStates":
			err = service.processPolicyStates(ctx, command)
		case "policyHelmCharts":
			err = service.processPolicyHelmCharts(command)
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

func (service *PollService) processEdgeStackCommand(ctx context.Context, command client.AsyncCommand) error {
	var stackData edge.StackPayload
	if err := mapstructure.Decode(command.Value, &stackData); err != nil {
		return newOperationError("stack", command.Operation, err)
	}

	switch command.Operation {
	case "add", "replace", "remove":
		if ac, ok := service.portainerClient.(*client.PortainerAsyncClient); ok {
			ac.SetPendingCommand(portainer.EdgeStackID(stackData.ID), stackData.Version, command.Timestamp)
		}
	}

	switch command.Operation {
	case "add", "replace":
		if err := service.edgeStackManager.DeployStack(ctx, stackData); err != nil {
			return service.portainerClient.SetEdgeStackStatus(stackData.ID, stackData.Version, portainer.EdgeStackStatusError, stackData.RollbackTo, fmt.Errorf("failed to deploy async stack: %w", err).Error())
		}
	case "remove":
		if err := service.edgeStackManager.DeleteStack(ctx, stackData); err != nil {
			return service.portainerClient.SetEdgeStackStatus(stackData.ID, stackData.Version, portainer.EdgeStackStatusError, stackData.RollbackTo, fmt.Errorf("failed to delete async stack: %w", err).Error())
		}
	default:
		return service.portainerClient.SetEdgeStackStatus(stackData.ID, stackData.Version, portainer.EdgeStackStatusError, stackData.RollbackTo, fmt.Errorf("operation not supported: %s", command.Operation).Error())
	}

	if err := service.portainerClient.SetEdgeStackStatus(stackData.ID, stackData.Version, portainer.EdgeStackStatusAcknowledged, stackData.RollbackTo, ""); err != nil {
		return newOperationError("stack", command.Operation, err)
	}

	return nil
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

	if err := mapstructure.Decode(command.Value, &logCmd); err != nil {
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

func (service *PollService) processNormalStackCommand(ctx context.Context, command client.AsyncCommand) error {
	var normalStackCommand client.NormalStackCommandData
	err := mapstructure.Decode(command.Value, &normalStackCommand)
	if err != nil {
		return newOperationError("normalStack", "n/a", err)
	}

	switch normalStackCommand.StackOperation {
	case "remove":
		err = service.edgeManager.stackManager.DeleteNormalStack(ctx, normalStackCommand.Name, normalStackCommand.RemoveVolumes)
	}

	return newOperationError("normalStack", command.Operation, err)
}
func (service *PollService) processEdgeConfigCommand(cmd client.AsyncCommand) error {
	var configData client.EdgeConfig
	err := mapstructure.Decode(cmd.Value, &configData)
	if err != nil {
		return newOperationError("edgeConfig", cmd.Operation, err)
	}

	if configData.Invalid {
		if err := service.portainerClient.SetEdgeConfigState(configData.ID, client.EdgeConfigFailureState); err != nil {
			log.Error().Err(err).Msg("failed to set edge config state")
		}

		err = errors.New("edge secret is not allowed to transmit over HTTP")
		return newOperationError("edgeConfig", cmd.Operation, err)
	}

	switch EdgeAsyncCommandOperation(cmd.Operation) {
	case EdgeAsyncCommandOpAdd:
		err = service.edgeManager.CreateEdgeConfig(&configData)
	case EdgeAsyncCommandOpReplace:
		err = service.edgeManager.UpdateEdgeConfig(&configData)
	case EdgeAsyncCommandOpRemove:
		err = service.edgeManager.DeleteEdgeConfig(&configData)
	}

	if err == nil {
		if err := service.portainerClient.SetEdgeConfigState(configData.ID, client.EdgeConfigIdleState); err != nil {
			log.Error().Err(err).Msg("failed to set edge config state")
		}
	} else if err := service.portainerClient.SetEdgeConfigState(configData.ID, client.EdgeConfigFailureState); err != nil {
		log.Error().Err(err).Msg("failed to set edge config state")
	}

	return newOperationError("edgeConfig", cmd.Operation, err)
}

// processPolicyStates handles the "policyStates" async command (per-policy payload).
// It caches any chart bundles for on-demand GetCharts, then drives the reconciler
// directly instead of the legacy PolicyManager.
func (service *PollService) processPolicyStates(ctx context.Context, cmd client.AsyncCommand) error {
	var payload client.PolicyStatesCommandPayload

	// Use JSON re-marshal/unmarshal rather than mapstructure. The server encodes
	// []byte fields (e.g. PolicyDesiredState.Config) as base64 strings in JSON;
	// mapstructure converts string→[]byte as raw string bytes (not base64-decoded),
	// which would corrupt Config and cause HelmHandler.Apply to fail.
	raw, err := segmentjson.Marshal(cmd.Value)
	if err != nil {
		return newOperationError("processPolicyStates", cmd.Operation, fmt.Errorf("re-marshal command value: %w", err))
	}
	if err := segmentjson.Unmarshal(raw, &payload); err != nil {
		return newOperationError("processPolicyStates", cmd.Operation, err)
	}

	// Cache chart bundles so HelmHandler.reconcileCharts can read them via GetCharts.
	// We intentionally reuse the same SetChartsResponse / PolicyHelmCharts cache path
	// as the legacy "policyHelmCharts" command — both old and new commands store
	// tarballs in the same in-memory cache, which GetCharts reads from on any
	// subsequent call. No separate cache is needed for the new command type.
	if len(payload.ChartBundles) > 0 {
		cacher, ok := service.portainerClient.(client.ChartCacher)
		if !ok {
			return newOperationError("processPolicyStates", cmd.Operation, errors.New("portainer client does not support chart caching"))
		}
		cacher.SetChartsResponse(&client.PolicyHelmCharts{
			PolicyChartBundles:    payload.ChartBundles,
			RestoreSettingsBundle: payload.RestoreBundle,
		})
	}

	service.reconcilePolicies(payload.States)
	return nil
}

func (service *PollService) processPolicyHelmCharts(cmd client.AsyncCommand) error {
	var policies client.PolicyHelmCharts

	if err := mapstructure.Decode(cmd.Value, &policies); err != nil {
		return newOperationError("processPolicyHelmCharts", cmd.Operation, err)
	}

	if cacher, ok := service.portainerClient.(client.ChartCacher); ok {
		cacher.SetChartsResponse(&policies)
	} else {
		return newOperationError("processPolicyHelmCharts", cmd.Operation, errors.New("portainer client does not support setting policy helm charts response"))
	}

	if service.policies == nil {
		service.policies = make(map[string]string)
	}

	for _, chart := range policies.PolicyChartBundles {
		switch EdgeAsyncCommandOperation(cmd.Operation) {
		case EdgeAsyncCommandOpAdd, EdgeAsyncCommandOpReplace:
			service.policies[chart.ChartName] = chart.Fingerprint

		case EdgeAsyncCommandOpRemove:
			delete(service.policies, chart.ChartName)
		}
	}

	var policyChartSummaries []portainer.PolicyChartSummary
	for chartName, fingerprint := range service.policies {
		policyChartSummaries = append(policyChartSummaries, portainer.PolicyChartSummary{
			ChartName:   chartName,
			Fingerprint: fingerprint,
		})
	}

	service.policyManager.ProcessPolicyHelmCharts(policyChartSummaries)

	return nil
}
