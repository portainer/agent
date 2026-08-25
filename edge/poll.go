package edge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/evaluator"
	"github.com/portainer/agent/edge/policies"
	"github.com/portainer/agent/edge/policies/helm"
	"github.com/portainer/agent/edge/scheduler"
	"github.com/portainer/agent/edge/stack"
	agentmetrics "github.com/portainer/agent/http/handler/metrics"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/policyreconcile"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/libcrypto"
	"github.com/portainer/portainer/pkg/librand"
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"

	prommodel "github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/rs/zerolog/log"
)

const (
	tunnelActivityCheckInterval = 30 * time.Second
	// TODO: metricCollectionInterval should be made configurable via a CLI flag
	metricCollectionInterval = 60 * time.Second
	globalKeyInUse           = 0
)

var collectRawMetricsFn = kubernetes.CollectRawMetrics
var collectNodeConditionsFn = kubernetes.CollectNodeConditions
var collectEtcdHealthFn = kubernetes.CollectEtcdHealth
var collectAPIServerCertFn = kubernetes.CollectAPIServerCert
var collectAPIServerHealthFn = kubernetes.CollectAPIServerHealth
var collectAPIServerLatencyFn = kubernetes.CollectAPIServerRequestLatency

func buildMetricsScrapeTarget(apiServerAddr string) string {
	if host, port, err := net.SplitHostPort(apiServerAddr); err == nil {
		if host == "" {
			apiServerAddr = net.JoinHostPort("localhost", port)
		} else if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			apiServerAddr = net.JoinHostPort("localhost", port)
		}
	}

	return (&url.URL{
		Scheme: "http",
		Host:   apiServerAddr,
		Path:   "/api/metrics",
	}).String()
}

func buildAlertmanagerTarget(portainerURL string, endpointID portainer.EndpointID) string {
	return strings.TrimRight(portainerURL, "/") + fmt.Sprintf("/api/endpoints/%d/edge/alerts", endpointID)
}

// PollService is used to poll a Portainer instance to retrieve the status associated to the Edge endpoint.
// It is responsible for managing the state of the reverse tunnel (open and closing after inactivity).
// It is also responsible for retrieving the data associated to Edge stacks and schedules.
type PollService struct {
	apiServerAddr            string
	pollIntervalInSeconds    float64
	pollTicker               *time.Ticker
	inactivityTimeout        time.Duration
	edgeID                   string
	portainerClient          client.PortainerClient
	tunnelClient             agent.ReverseTunnelClient
	scheduleManager          agent.Scheduler
	policyManager            *policies.PolicyManager
	lastActivity             time.Time
	updateLastActivitySignal chan struct{}
	startSignal              chan struct{}
	stopSignal               chan struct{}
	metricPushCancel         context.CancelFunc
	edgeManager              *Manager
	edgeStackManager         *stack.StackManager
	portainerURL             string
	tunnelServerAddr         string
	tunnelServerFingerprint  string
	tunnelProxy              string
	firstPoll                bool
	alertRules               []pkgmetrics.EdgeAlertRule
	alertRulesYAML           string
	alertRulesHash           uint64
	invalidAlertRulesHash    *uint64
	configReloadError        string
	evaluator                *evaluator.Service
	evaluatorInitAttempted   bool
	metricsHandler           *agentmetrics.Handler
	reconciler               *policyreconcile.Reconciler
	pollHooks                []policyreconcile.PollHook
	chartReporter            *helm.ChartStatusReporter // nil on Docker/Podman agents
	policyReconcileCh        chan []portainer.PolicyDesiredState
	// Async mode only
	pingInterval     time.Duration
	snapshotInterval time.Duration
	commandInterval  time.Duration
	pingTicker       *time.Ticker
	snapshotTicker   *time.Ticker
	commandTicker    *time.Ticker
	policies         map[string]string // Name -> Fingerprint
}

type pollServiceConfig struct {
	APIServerAddr           string
	EdgeID                  string
	InactivityTimeout       string
	PollFrequency           string
	TunnelCapability        bool
	PortainerURL            string
	TunnelServerAddr        string
	TunnelServerFingerprint string
	TunnelProxy             string
	ContainerPlatform       agent.ContainerPlatform
}

// newPollService returns a pointer to a new instance of PollService, and will start two loops in go routines.
// The first loop will poll the Portainer instance for the status of the associated endpoint and create a reverse tunnel
// if needed as well as manage schedules.
// The second loop will check for the last activity of the reverse tunnel and close the tunnel if it exceeds the tunnel
// inactivity duration.
// If TunnelCapability is disabled, it will only poll for Edge stacks and schedule without managing reverse tunnels.
func newPollService(edgeManager *Manager, edgeStackManager *stack.StackManager, logsManager *scheduler.LogsManager, config *pollServiceConfig, portainerClient client.PortainerClient, policyManager *policies.PolicyManager, edgeAsyncMode bool) (*PollService, error) {
	pollFrequency, err := time.ParseDuration(config.PollFrequency)
	if err != nil {
		return nil, err
	}

	inactivityTimeout, err := time.ParseDuration(config.InactivityTimeout)
	if err != nil {
		return nil, err
	}

	pollService := &PollService{
		apiServerAddr:            config.APIServerAddr,
		edgeID:                   config.EdgeID,
		pollIntervalInSeconds:    pollFrequency.Seconds(),
		inactivityTimeout:        inactivityTimeout,
		scheduleManager:          scheduler.NewCronManager(logsManager),
		updateLastActivitySignal: make(chan struct{}),
		startSignal:              make(chan struct{}),
		stopSignal:               make(chan struct{}),
		edgeManager:              edgeManager,
		policyManager:            policyManager,
		edgeStackManager:         edgeStackManager,
		portainerURL:             config.PortainerURL,
		tunnelServerAddr:         config.TunnelServerAddr,
		tunnelServerFingerprint:  config.TunnelServerFingerprint,
		tunnelProxy:              config.TunnelProxy,
		portainerClient:          portainerClient,
		firstPoll:                true,
		metricsHandler:           edgeManager.MetricsHandler(),
		reconciler:               policyreconcile.NewReconciler(),
		policyReconcileCh:        make(chan []portainer.PolicyDesiredState, 1),
	}

	if config.TunnelCapability {
		pollService.tunnelClient = chisel.NewClient(
			edgeManager.agentOptions.SSLCACert,
			edgeManager.agentOptions.SSLCert,
			edgeManager.agentOptions.SSLKey,
		)
	}

	if edgeAsyncMode {
		go pollService.startStatusPollLoopAsync()
	} else {
		pollService.pollTicker = time.NewTicker(pollFrequency)

		go pollService.startPolicyReconcileWorker()
		go pollService.startStatusPollLoop()
		go pollService.startActivityMonitoringLoop()

		if config.ContainerPlatform == agent.PlatformKubernetes && edgeManager.kubeClient != nil {
			ctx, cancel := context.WithCancel(context.Background())
			pollService.metricPushCancel = cancel
			go pollService.startMetricPushLoop(ctx)
		}
	}

	return pollService, nil
}

// RegisterPolicy registers a policy type's factory and poll hooks with the reconciler.
// This is the preferred registration method for new policy types.
func (service *PollService) RegisterPolicy(reg policyreconcile.Registration) {
	service.reconciler.RegisterFactory(reg.Type, reg.Factory)
	service.pollHooks = append(service.pollHooks, reg.PollHooks...)
}

// SetChartReporter registers the helm ChartStatusReporter used for legacy dual-emit.
// Called at boot on K8s agents only; nil on Docker/Podman agents.
// This is helm-specific and cannot be folded into Registration because the
// dual-emit path consumes a concrete *helm.ChartStatusReporter.
func (service *PollService) SetChartReporter(r *helm.ChartStatusReporter) {
	service.chartReporter = r
}

// RegisterPollHook registers a PollHook that will be called every poll cycle via Tick.
// Intended for handler packages (e.g. helm.RestoreCoordinator) that need cross-poll
// work outside the reconciler's per-cycle Apply/Remove lifecycle.
func (service *PollService) RegisterPollHook(h policyreconcile.PollHook) {
	service.pollHooks = append(service.pollHooks, h)
}
func (service *PollService) resetActivityTimer() {
	if service.tunnelClient != nil && service.tunnelClient.IsTunnelOpen() {
		service.updateLastActivitySignal <- struct{}{}
	}
}

func (service *PollService) Start() {
	service.startSignal <- struct{}{}
}

func (service *PollService) Stop() {
	service.stopSignal <- struct{}{}
	if service.metricPushCancel != nil {
		service.metricPushCancel()
	}
	if service.metricsHandler != nil {
		service.metricsHandler.ClearMetrics()
	}
	service.portainerClient.SetAlertState(nil)
	if service.evaluator != nil {
		service.evaluator.Stop()
	}
}

func (service *PollService) startStatusPollLoop() {
	var pollCh <-chan time.Time

	log.Debug().
		Float64("poll_interval_seconds", service.pollIntervalInSeconds).
		Str("server_url", service.portainerURL).
		Msg("starting Portainer short-polling client")

	lastPollFailed := false

	for {
		select {
		case <-pollCh:
			// Jitter
			if lastPollFailed {
				lastPollFailed = false
				t := time.Duration(librand.Float64() * service.pollIntervalInSeconds * float64(time.Second))
				time.Sleep(t)
				service.pollTicker.Reset(time.Duration(service.pollIntervalInSeconds) * time.Second)
			}

			err := service.poll()
			if err != nil {
				log.Error().Err(err).Msg("an error occurred during short poll")
				lastPollFailed = true
				service.pollTicker.Reset(time.Duration(service.pollIntervalInSeconds) * time.Second)
			}
		case <-service.startSignal:
			pollCh = service.pollTicker.C
		case <-service.stopSignal:
			log.Debug().Msg("stopping Portainer short-polling client")
			pollCh = nil
		}
	}
}

func (service *PollService) startActivityMonitoringLoop() {
	ticker := time.NewTicker(tunnelActivityCheckInterval)

	log.Debug().
		Float64("monitoring_interval_seconds", tunnelActivityCheckInterval.Seconds()).
		Str("inactivity_timeout", service.inactivityTimeout.String()).
		Msg("")

	for {
		select {
		case <-ticker.C:
			if service.lastActivity.IsZero() {
				continue
			}

			elapsed := time.Since(service.lastActivity)

			log.Debug().
				Float64("tunnel_last_activity_seconds", elapsed.Seconds()).
				Msg("tunnel activity monitoring")

			if service.tunnelClient.IsTunnelOpen() && service.tunnelClient.CertsNeedRotation() {
				log.Info().
					Float64("tunnel_last_activity_seconds", elapsed.Seconds()).
					Msg("shutting down tunnel to rotate certificates")

				err := service.tunnelClient.CloseTunnel()
				if err != nil {
					log.Error().Err(err).Msg("unable to shutdown tunnel")
				}
			}

			if service.tunnelClient != nil && service.tunnelClient.IsTunnelOpen() && elapsed.Seconds() > service.inactivityTimeout.Seconds() {
				log.Info().
					Float64("tunnel_last_activity_seconds", elapsed.Seconds()).
					Msg("shutting down tunnel after inactivity period")

				err := service.tunnelClient.CloseTunnel()
				if err != nil {
					log.Error().Err(err).Msg("unable to shutdown tunnel")
				}
			}
		case <-service.updateLastActivitySignal:
			service.lastActivity = time.Now()
		}
	}
}

func (service *PollService) poll() error {
	if service.edgeManager.GetEndpointID() == globalKeyInUse {
		endpointID, err := service.portainerClient.GetEnvironmentID()
		if err != nil {
			return err
		}

		service.edgeManager.SetEndpointID(endpointID)
	}

	environmentStatus, err := service.portainerClient.GetEnvironmentStatus()
	if err != nil {
		var nonOkError *client.NonOkResponseError
		if errors.As(err, &nonOkError) {
			service.edgeManager.SetEndpointID(globalKeyInUse)
			service.edgeStackManager.ResetStacks()
		}

		return err
	}

	log.Debug().
		Str("status", environmentStatus.Status).
		Int("port", environmentStatus.Port).
		Int("schedule_count", len(environmentStatus.Schedules)).
		Float64("checkin_interval_seconds", environmentStatus.CheckinInterval).
		Msg("")

	if err := service.manageUpdateTunnel(*environmentStatus); err != nil {
		return err
	}

	service.processSchedules(environmentStatus.Schedules)

	if environmentStatus.CheckinInterval > 0 && environmentStatus.CheckinInterval != service.pollIntervalInSeconds {
		log.Debug().
			Float64("old_interval", service.pollIntervalInSeconds).
			Float64("new_interval", environmentStatus.CheckinInterval).
			Msg("updating poll interval")

		service.pollIntervalInSeconds = environmentStatus.CheckinInterval
		service.portainerClient.SetTimeout(time.Duration(environmentStatus.CheckinInterval) * time.Second)
		service.pollTicker.Reset(time.Duration(service.pollIntervalInSeconds) * time.Second)
	}

	service.processEdgeConfigs(environmentStatus.EdgeConfigurations)

	if hasPerPolicyPayload(environmentStatus) {
		// Keep Helm installs off the poll loop; if reconcile falls behind, only
		// the latest pending payload is kept.
		service.enqueuePolicyReconcile(*environmentStatus.PolicyStates)
	} else if service.edgeManager.kubeClient != nil && environmentStatus.PolicyChartSummaries != nil {
		// Legacy payload fallback: older server sends only per-chart summaries.
		// Absent payloads leave active handlers alone.
		go service.policyManager.ProcessPolicyHelmCharts(environmentStatus.PolicyChartSummaries)
	}

	service.alertRules = environmentStatus.AlertRules
	service.alertRulesYAML = environmentStatus.AlertRulesYAML

	if service.evaluator == nil && !service.evaluatorInitAttempted {
		service.evaluatorInitAttempted = true
		if service.edgeManager.kubeClient != nil {
			service.tryInitEvaluator()
			if service.evaluator != nil {
				log.Info().Msg("evaluator successfully initialized")
			} else {
				log.Warn().Msg("evaluator initialization failed, will not retry until restart")
			}
		} else {
			log.Debug().Msg("evaluator not initialized: kubeClient is nil")
		}
	}
	service.maybeReloadRules()
	service.publishAlertState()

	return service.processStacks(environmentStatus.Stacks)
}

func (service *PollService) manageUpdateTunnel(environmentStatus client.PollStatusResponse) error {
	if service.tunnelClient == nil {
		return nil
	}

	if environmentStatus.Status == agent.TunnelStatusIdle && service.tunnelClient.IsTunnelOpen() {
		log.Debug().
			Str("status", environmentStatus.Status).
			Msg("idle status detected, shutting down tunnel")

		if err := service.tunnelClient.CloseTunnel(); err != nil {
			log.Error().Err(err).Msg("unable to shutdown tunnel")
		}
	}

	if environmentStatus.Status == agent.TunnelStatusRequired && !service.tunnelClient.IsTunnelOpen() {
		log.Debug().Msg("required status detected, creating reverse tunnel")

		if err := service.createTunnel(environmentStatus.Credentials, environmentStatus.Port); err != nil {
			log.Error().Err(err).Msg("unable to create tunnel")

			return err
		}
	}

	return nil
}

func (service *PollService) createTunnel(encodedCredentials string, remotePort int) error {
	decodedCredentials, err := base64.RawStdEncoding.DecodeString(encodedCredentials)
	if err != nil {
		return err
	}

	credentials, err := libcrypto.Decrypt(decodedCredentials, []byte(service.edgeID))
	if err != nil {
		return err
	}

	tunnelConfig := agent.TunnelConfig{
		LocalAddr:         service.apiServerAddr,
		ServerAddr:        service.tunnelServerAddr,
		ServerFingerprint: service.tunnelServerFingerprint,
		Proxy:             service.tunnelProxy,
		Credentials:       string(credentials),
		RemotePort:        strconv.Itoa(remotePort),
	}

	if err := service.tunnelClient.CreateTunnel(tunnelConfig); err != nil {
		return err
	}

	service.resetActivityTimer()
	return nil
}

func (service *PollService) processSchedules(schedules []agent.Schedule) {
	if err := service.scheduleManager.Schedule(schedules); err != nil {
		log.Error().Err(err).Msg("an error occurred during schedule management")
	}
}

func (service *PollService) processStacks(pollResponseStacks []client.StackStatus) error {
	// Load existing edge stacks so they can be removed using the initial poll response
	if service.firstPoll {
		log.Info().Msg("loading the existing edge stacks")

		ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
		defer cancelFn()

		if err := service.edgeStackManager.LoadExistingEdgeStacks(ctx); err == nil {
			service.firstPoll = false
		} else {
			log.Warn().Err(err).Msg("unable to retrieve the existing edge stacks")
		}
	}

	stacks := map[int]client.StackStatus{}
	for _, s := range pollResponseStacks {
		stacks[s.ID] = s
	}

	if err := service.edgeStackManager.UpdateStacksStatus(stacks); err != nil {
		log.Error().Err(err).Msg("an error occurred during stack management")

		return err
	}

	return nil
}

func (service *PollService) processEdgeConfig(fn func(*client.EdgeConfig) error, edgeConfigID client.EdgeConfigID) {
	edgeConfig, err := service.portainerClient.GetEdgeConfig(edgeConfigID)
	if err != nil {
		log.Error().Err(err).Msg("an error occurred while retrieving an edge configuration")

		if strings.Contains(err.Error(), "forbidden") {
			if err := service.portainerClient.SetEdgeConfigState(edgeConfigID, client.EdgeConfigFailureState); err != nil {
				log.Error().Err(err).Msg("an error occurred while updating the edge configuration state")
			}
		}

		return
	}

	newState := client.EdgeConfigIdleState

	if err := fn(edgeConfig); err != nil {
		log.Error().Err(err).Msg("an error occurred while creating an edge configuration")

		newState = client.EdgeConfigFailureState
	}

	if err := service.portainerClient.SetEdgeConfigState(edgeConfigID, newState); err != nil {
		log.Error().Err(err).Msg("an error occurred while updating the edge configuration state")
	}
}

func (service *PollService) processEdgeConfigs(edgeConfigs map[client.EdgeConfigID]client.EdgeConfigStateType) {
	for id, state := range edgeConfigs {
		log.Debug().Int("edge_config_id", int(id)).Stringer("state", state).Msg("processing edge config")

		switch state {

		case client.EdgeConfigSavingState:
			service.processEdgeConfig(service.edgeManager.CreateEdgeConfig, id)

		case client.EdgeConfigDeletingState:
			service.processEdgeConfig(service.edgeManager.DeleteEdgeConfig, id)

		case client.EdgeConfigUpdatingState:
			service.processEdgeConfig(service.edgeManager.UpdateEdgeConfig, id)
		}
	}
}

// startMetricPushLoop runs a background loop that collects Kubernetes performance
// metrics and evaluates alert rules on each tick. It exits when ctx is cancelled.
func (service *PollService) startMetricPushLoop(ctx context.Context) {
	log.Info().
		Dur("interval", metricCollectionInterval).
		Msg("metric-tick: starting metric push loop")

	// Fire immediately on first tick instead of waiting a full interval.
	service.pushPerformanceMetrics(ctx)

	ticker := time.NewTicker(metricCollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("metric-tick: context cancelled, stopping metric push loop")
			return
		case <-ticker.C:
			service.pushPerformanceMetrics(ctx)
		}
	}
}

// pushPerformanceMetrics collects raw cluster metrics from the Kubernetes API and
// updates the metrics handler gauges. The evaluator's scrape loop will pick them up.
func (service *PollService) pushPerformanceMetrics(ctx context.Context) {
	log.Debug().Msg("metric-tick: pushPerformanceMetrics called")

	if service.metricsHandler == nil {
		log.Debug().Msg("metric-tick: metricsHandler is nil, skipping")
		return
	}

	raw, rawErr := collectRawMetricsFn(ctx, service.edgeManager.kubeClient)
	if rawErr != nil {
		service.metricsHandler.ClearMetrics()
		log.Warn().Err(rawErr).Msg("metric-tick: failed to collect K8s raw metrics, cleared published snapshot")
	} else {
		log.Debug().
			Bool("has_cpu", raw.HasCPU).
			Bool("has_memory", raw.HasMemory).
			Bool("has_disk", raw.HasDisk).
			Bool("has_network", raw.HasNetwork).
			Msg("metric-tick: collected raw metrics, updating gauges")

		service.metricsHandler.UpdateMetrics(raw)
	}

	statuses, nodeErr := collectNodeConditionsFn(ctx, service.edgeManager.kubeClient)
	if nodeErr != nil {
		service.metricsHandler.ClearNodeMetrics()
		log.Warn().Err(nodeErr).Msg("metric-tick: failed to collect node conditions, cleared node readiness gauges")
	} else {
		service.metricsHandler.UpdateNodeMetrics(statuses)
		log.Debug().Int("node_count", len(statuses)).Msg("metric-tick: node readiness gauges updated")
	}

	etcdHealthy, etcdErr := collectEtcdHealthFn(ctx, service.edgeManager.kubeClient)
	if etcdErr != nil {
		service.metricsHandler.ClearEtcdMetrics()
		log.Debug().Err(etcdErr).Msg("metric-tick: etcd health indeterminate, marked gauge as unknown")
	} else {
		service.metricsHandler.UpdateEtcdMetrics(etcdHealthy)
		log.Debug().Bool("healthy", etcdHealthy).Msg("metric-tick: etcd health gauge updated")
	}

	cert, certErr := collectAPIServerCertFn(ctx, service.edgeManager.kubeClient)
	if certErr != nil {
		service.metricsHandler.ClearAPIServerTLSCertMetrics()
		log.Warn().Err(certErr).Msg("metric-tick: failed to collect API server TLS cert, cleared tls cert gauges")
	} else {
		service.metricsHandler.UpdateAPIServerTLSCertMetrics([]kubernetes.TLSCertInfo{*cert})
		log.Debug().Str("source", cert.Source).Str("cn", cert.CN).Msg("metric-tick: tls cert gauges updated")
	}

	apiServerHealthy := collectAPIServerHealthFn(ctx, service.edgeManager.kubeClient)
	service.metricsHandler.UpdateAPIServerHealthMetrics(apiServerHealthy)
	log.Debug().Bool("healthy", apiServerHealthy).Msg("metric-tick: API server health gauge updated")

	latencyHistogram, latencyErr := collectAPIServerLatencyFn(ctx, service.edgeManager.kubeClient)
	if latencyErr != nil {
		if errors.Is(latencyErr, kubernetes.ErrAPIServerRequestLatencyUnsupported) {
			service.metricsHandler.ClearAPIServerLatencyMetrics()
			log.Debug().Err(latencyErr).Msg("metric-tick: API server request latency unsupported, cleared latency gauges")
		} else {
			// Transient scrape error: keep the last published cumulative buckets so
			// the evaluator's rate() window tolerates the gap rather than resetting.
			log.Warn().Err(latencyErr).Msg("metric-tick: failed to collect API server request latency, retaining last latency gauges")
		}
	} else {
		service.metricsHandler.UpdateAPIServerLatencyMetrics(latencyHistogram)
		log.Debug().Int("bucket_count", len(latencyHistogram.Buckets)).Msg("metric-tick: API server request latency gauges updated")
	}
}

func (service *PollService) publishAlertState() {
	var next *pkgmetrics.EdgeAlertState
	if service.evaluator != nil {
		next = buildEdgeAlertState(service.evaluator.AlertState(), service.configReloadError)
	}

	service.portainerClient.SetAlertState(next)
}

func buildEdgeAlertState(states []pkgmetrics.EdgeAlertRuleState, configReloadError string) *pkgmetrics.EdgeAlertState {
	if len(states) == 0 && configReloadError == "" {
		return nil
	}

	sort.Slice(states, func(i, j int) bool {
		return states[i].RuleID < states[j].RuleID
	})

	return &pkgmetrics.EdgeAlertState{
		Rules:             states,
		ConfigReloadError: configReloadError,
	}
}

// tryInitEvaluator creates and starts a new evaluator.Service for Prometheus rule evaluation.
func (service *PollService) tryInitEvaluator() {
	log.Debug().Msg("tryInitEvaluator: called")

	if service.edgeManager.GetEndpointID() == globalKeyInUse {
		log.Debug().Msg("tryInitEvaluator: endpoint ID is globalKeyInUse, skipping")
		return
	}

	endpointID := service.edgeManager.GetEndpointID()
	dataDir := filesystem.JoinPaths(service.edgeManager.agentOptions.DataPath, "alerting")

	scrapeTarget := buildMetricsScrapeTarget(service.apiServerAddr)
	alertmanagerTarget := buildAlertmanagerTarget(service.portainerURL, endpointID)
	eval, err := evaluator.New(evaluator.Config{
		DataDir:            dataDir,
		EndpointID:         endpointID,
		ScrapeTarget:       scrapeTarget,
		AlertmanagerTarget: alertmanagerTarget,
		AlertmanagerHeaders: map[string]string{
			agent.HTTPEdgeIdentifierHeaderName: service.edgeID,
		},
		InsecureSkipVerify: service.edgeManager.agentOptions.EdgeInsecurePoll,
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to create alert rule evaluator")
		return
	}

	eval.Start()
	service.evaluator = eval

	log.Info().
		Int("endpoint_id", int(endpointID)).
		Str("data_dir", dataDir).
		Msg("alert rule evaluator started")
}

// maybeReloadRules writes pre-compiled YAML from the server to disk and reloads
// the evaluator if the rule set has changed since the last poll.
// It validates the incoming YAML before writing, backs up the current file,
// and rolls back on reload failure.
func (service *PollService) maybeReloadRules() {
	if service.evaluator == nil {
		return
	}

	h := computeRulesHash(service.alertRulesYAML)
	if h == service.alertRulesHash {
		log.Debug().Msg("poll: alert rules unchanged, skipping reload")
		return
	}

	if service.invalidAlertRulesHash != nil && h == *service.invalidAlertRulesHash {
		log.Debug().Msg("poll: alert rules unchanged since previous validation failure, skipping reload")
		return
	}

	log.Debug().
		Int("rule_count", len(service.alertRules)).
		Bool("has_yaml", service.alertRulesYAML != "").
		Msg("poll: alert rules changed, writing to disk")

	alertsDir := filesystem.JoinPaths(service.edgeManager.agentOptions.DataPath, "alerting")
	alertsFile := filesystem.JoinPaths(alertsDir, "alerts.yaml")
	backupFile := alertsFile + ".bak"

	if service.alertRulesYAML == "" {
		if err := service.evaluator.ReloadRules(""); err != nil {
			log.Error().Err(err).Msg("failed to reload alert rules (clear)")
			service.setReloadError("clear reload failed: " + err.Error())
			return
		}
		// Delete files only after the reload succeeds so we can roll back on failure.
		_ = os.Remove(alertsFile)
		_ = os.Remove(backupFile)
		service.alertRulesHash = h
		service.invalidAlertRulesHash = nil
		service.setReloadError("")
		log.Info().Msg("alert rules cleared")
		return
	}

	// Validate incoming YAML before writing to disk.
	if _, errs := rulefmt.Parse([]byte(service.alertRulesYAML), false, prommodel.UTF8Validation, parser.NewParser(parser.Options{})); len(errs) > 0 {
		errMsg := "invalid alert rules YAML from server: " + errs[0].Error()
		service.invalidAlertRulesHash = &h
		log.Error().Str("error", errs[0].Error()).Msg("poll: received invalid alert rules YAML, skipping write")
		service.setReloadError(errMsg)
		return
	}

	if err := os.MkdirAll(alertsDir, 0o750); err != nil {
		log.Error().Err(err).Msg("failed to create alerting directory")
		service.setReloadError("create alerting dir: " + err.Error())
		return
	}

	// Back up existing alerts.yaml before overwriting (ignore if file doesn't exist).
	if err := copyFile(alertsFile, backupFile); err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Msg("failed to back up alerts.yaml, proceeding anyway")
	}

	// Atomic write: temp file + rename
	tmpFile := alertsFile + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(service.alertRulesYAML), 0o600); err != nil {
		log.Error().Err(err).Msg("failed to write alerts.yaml temp file")
		service.setReloadError("write temp file: " + err.Error())
		return
	}
	if err := os.Rename(tmpFile, alertsFile); err != nil {
		log.Error().Err(err).Msg("failed to rename alerts.yaml temp file")
		service.setReloadError("rename temp file: " + err.Error())
		return
	}

	if err := service.evaluator.ReloadRules(alertsFile); err != nil {
		log.Error().Err(err).Msg("failed to reload alert rules, attempting rollback")

		// Rollback: restore backup and reload with previous rules.
		if restoreErr := restoreBackup(backupFile, alertsFile); restoreErr != nil {
			log.Error().Err(restoreErr).Msg("rollback: failed to restore alerts.yaml.bak")
			service.setReloadError("reload failed and rollback failed: " + err.Error())
			return
		}
		if rollbackErr := service.evaluator.ReloadRules(alertsFile); rollbackErr != nil {
			log.Error().Err(rollbackErr).Msg("rollback: failed to reload restored rules")
			service.setReloadError("reload failed and rollback reload failed: " + err.Error())
			return
		}
		log.Warn().Msg("rollback: successfully restored previous alert rules")
		service.setReloadError("reload failed, rolled back to previous rules: " + err.Error())
		return
	}

	_ = os.Remove(backupFile)
	service.alertRulesHash = h
	service.invalidAlertRulesHash = nil
	service.setReloadError("")
	log.Info().Int("rule_count", len(service.alertRules)).Msg("alert rules reloaded from YAML")
}

func (service *PollService) setReloadError(errMsg string) {
	service.configReloadError = errMsg
}

// copyFile copies src to dst, creating or truncating dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// restoreBackup moves the backup file back to the original path.
func restoreBackup(backupPath, originalPath string) error {
	if err := os.Rename(backupPath, originalPath); err != nil {
		return fmt.Errorf("restore backup failed: %w", err)
	}
	return nil
}

// computeRulesHash returns an FNV-64a hash over the YAML content that is
// actually written to disk, so change-detection matches the deployed artifact.
func computeRulesHash(yamlContent string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(yamlContent))
	return h.Sum64()
}

// reconcilePolicies drives the reconciler and any registered PollHooks for a
// per-policy desired-state payload. Called from both the sync poll loop and the
// async "policyStates" command handler so both paths share the same dispatch logic.
// Reconcile is synchronous so Statuses() reflects the completed cycle when it returns.
func (service *PollService) reconcilePolicies(states []portainer.PolicyDesiredState) {
	// poll() has no context parameter (legacy design); Background is consistent
	// with other calls in the same function.
	ctx := context.Background()
	desiredStates := toDesiredStates(states)
	service.reconciler.Reconcile(ctx, desiredStates)

	desiredIDs := desiredStateIDs(desiredStates)
	var hookStatuses []policyreconcile.ActualState
	for _, h := range service.pollHooks {
		hookStatuses = append(hookStatuses, h.Tick(ctx, desiredIDs)...)
	}

	// New per-policy status endpoint.
	// Hook statuses override the reconciler's base entries (last-wins dedup by policy ID).
	allStatuses := deduplicateStatuses(append(service.reconciler.Statuses(), hookStatuses...))
	if err := service.portainerClient.ReportPolicyStatuses(toPolicyActualStates(allStatuses)); err != nil {
		log.Debug().Err(err).Str("context", "PolicyStatusReporting").Msg("ReportPolicyStatuses failed")
	}

	// Legacy dual-emit: feeds the old per-chart endpoint for servers without the new one.
	if service.chartReporter != nil {
		snapshot := helm.ChartStatusSnapshot(service.chartReporter)
		for i := range snapshot {
			snapshot[i].EnvironmentID = service.edgeManager.GetEndpointID()
		}
		if err := service.portainerClient.UpdatePolicyChartStatuses(snapshot); err != nil {
			log.Debug().Err(err).Str("context", "PolicyStatusReporting").Msg("UpdatePolicyChartStatuses (dual-emit) failed")
		}
	}
}

func (service *PollService) enqueuePolicyReconcile(states []portainer.PolicyDesiredState) {
	if service.policyReconcileCh == nil {
		service.reconcilePolicies(states)
		return
	}

	queued := append([]portainer.PolicyDesiredState(nil), states...)
	select {
	case service.policyReconcileCh <- queued:
	default:
		// Channel full — drain the stale payload and replace with the latest.
		// This is safe because the worker always calls latestPolicyReconcileState
		// which drains any remaining items before processing, so at most one
		// intermediate payload is dropped per poll cycle.
		log.Debug().
			Str("context", "PolicyReconcileEnqueue").
			Msg("Reconcile channel full, replacing pending payload with latest")
		select {
		case <-service.policyReconcileCh:
		default:
		}
		select {
		case service.policyReconcileCh <- queued:
		default:
			log.Warn().
				Str("context", "PolicyReconcileEnqueue").
				Msg("Failed to enqueue policy reconcile after drain, payload dropped")
		}
	}
}

func (service *PollService) startPolicyReconcileWorker() {
	for states := range service.policyReconcileCh {
		service.reconcilePolicies(service.latestPolicyReconcileState(states))
	}
}

func (service *PollService) latestPolicyReconcileState(states []portainer.PolicyDesiredState) []portainer.PolicyDesiredState {
	for {
		select {
		case latest := <-service.policyReconcileCh:
			states = latest
		default:
			return states
		}
	}
}

// toPolicyActualStates converts reconciler ActualState entries to the payload type
// sent to the server via ReportPolicyStatuses.
func toPolicyActualStates(states []policyreconcile.ActualState) []portainer.PolicyActualState {
	out := make([]portainer.PolicyActualState, len(states))
	for i, s := range states {
		out[i] = portainer.PolicyActualState{
			PolicyID:    s.PolicyID,
			Type:        s.Type,
			Fingerprint: s.Fingerprint,
			Status:      string(s.Status),
			Message:     s.Message,
		}
	}
	return out
}

// deduplicateStatuses keeps the last entry per PolicyID. PollHook statuses are
// appended after reconciler statuses, so "last wins" gives hooks priority — their
// enriched messages override the reconciler's default empty-message entries.
func deduplicateStatuses(states []policyreconcile.ActualState) []policyreconcile.ActualState {
	seen := make(map[portainer.PolicyID]int, len(states))
	out := make([]policyreconcile.ActualState, 0, len(states))
	for _, s := range states {
		if idx, exists := seen[s.PolicyID]; exists {
			out[idx] = s
		} else {
			seen[s.PolicyID] = len(out)
			out = append(out, s)
		}
	}
	return out
}

// toDesiredStates converts the payload PolicyDesiredState slice to the reconciler's
// internal DesiredState type. json.RawMessage and []byte are both byte slices.
func toDesiredStates(states []portainer.PolicyDesiredState) []policyreconcile.DesiredState {
	out := make([]policyreconcile.DesiredState, len(states))
	for i, s := range states {
		out[i] = policyreconcile.DesiredState{
			PolicyID:    s.PolicyID,
			Type:        s.Type,
			Fingerprint: s.Fingerprint,
			Config:      s.Config,
		}
	}
	return out
}

// hasPerPolicyPayload reports whether the server sent the per-policy payload field.
// A non-nil pointer (even to an empty slice) means the server supports the new
// format — the agent must run reconcilePolicies regardless of whether the slice is
// empty, because an empty desired list signals "all policies deleted: remove all
// active handlers". A nil pointer means the field was absent (old server) → use
// the legacy per-chart path.
func hasPerPolicyPayload(status *client.PollStatusResponse) bool {
	return status.PolicyStates != nil
}

func desiredStateIDs(states []policyreconcile.DesiredState) []portainer.PolicyID {
	ids := make([]portainer.PolicyID, len(states))
	for i, s := range states {
		ids[i] = s.PolicyID
	}
	return ids
}
