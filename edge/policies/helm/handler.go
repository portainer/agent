package helm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/api/logs"
	"github.com/portainer/portainer/pkg/libhelm/options"
	libhelmtypes "github.com/portainer/portainer/pkg/libhelm/types"
	"github.com/portainer/portainer/pkg/libpolicy"
	"github.com/rs/zerolog/log"

	agentdeployer "github.com/portainer/agent/deployer"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/policyreconcile"
)

// chartRecord tracks the known state of a single Helm release managed by this handler.
type chartRecord struct {
	ChartName string
	// ReleaseName is the Helm release name, which may differ from ChartName when the
	// bundle sets ReleaseName (e.g. "portainer-observability-k8s" installs as "kubernetes-agent").
	// Empty until first install; callers fall back to ChartName via releaseNameForRecord.
	ReleaseName string
	Fingerprint string
	Namespace   string
	Status      portainer.HelmInstallStatus
	Message     string
}

// externalReleaseConflictMessage is reported when a Helm release with the target name
// already exists but is not managed by Portainer (no portainer/chart-path annotation).
const externalReleaseConflictMessage = "A Helm release with this name already exists but is not managed by Portainer. Uninstall the existing release before applying this policy."

// maxChartHistory caps how many old revisions Helm keeps per policy chart release.
// Left unset, Helm keeps every revision forever; namespaceDriftSensitiveCharts get
// re-upgraded every time HelmHandler.checkNamespaceDrift detects a live namespace
// change, which over an agent's lifetime would otherwise grow the release
// history without bound (see C9S-325).
const maxChartHistory = 20

// HelmPolicyConfig is a type alias (= not a new type) for portainer.HelmPolicyConfig.
// The canonical definition lives in CE portainer.go so both server-ee and agent share
// the same type. The alias means no conversion is needed between them.
type HelmPolicyConfig = portainer.HelmPolicyConfig

const helmPolicyType = "helm-k8s"

// Registration returns a policyreconcile.Registration for helm-k8s policies.
// coordinator (PollHook for restore retries) and nsDriftCoordinator (PollHook
// that re-applies namespace-drift-sensitive charts, see C9S-325) are both
// pre-created shared instances — pass the same nsDriftCoordinator given to
// NewPolicyManager so a policy dispatched via either path gets drift reapply.
// Caller must still call SetChartReporter on the PollService for legacy dual-emit.
func Registration(
	kube *kubernetes.KubeClient,
	helm libhelmtypes.HelmPackageManager,
	pc client.PortainerClient,
	coordinator *RestoreCoordinator,
	reporter *ChartStatusReporter,
	nsDriftCoordinator *NamespaceDriftCoordinator,
) policyreconcile.Registration {
	pollHooks := []policyreconcile.PollHook{coordinator}
	if nsDriftCoordinator != nil {
		pollHooks = append(pollHooks, nsDriftCoordinator)
	}

	return policyreconcile.Registration{
		Type:      helmPolicyType,
		Factory:   NewHandler(kube, helm, pc, coordinator, reporter, nsDriftCoordinator),
		PollHooks: pollHooks,
	}
}

// HelmHandler implements policyreconcile.PolicyHandler for Helm-based K8s policies.
// One instance is created per active policy ID by the HandlerFactory.
type HelmHandler struct {
	policyID           portainer.PolicyID
	kubeClient         *kubernetes.KubeClient
	helmPackageManager libhelmtypes.HelmPackageManager
	portainerClient    client.PortainerClient
	coordinator        *RestoreCoordinator
	chartReporter      *ChartStatusReporter       // may be nil on non-K8s agents
	nsDriftCoordinator *NamespaceDriftCoordinator // may be nil (e.g. in tests)
	namespaceLister    NamespaceLister            // nil (e.g. kubeClient is nil) disables drift checks for this handler

	mu              sync.Mutex
	installedCharts map[string]chartRecord                 // keyed by chartName; most policies 1:1, SecurityK8s has 2
	lastBundles     map[string]portainer.PolicyChartBundle // keyed by chartName; cached for namespace-drift reapply
	lastNamespaces  map[string]string                      // last-seen namespace snapshot; see checkNamespaceDrift
	pendingRestore  *portainer.RestoreSettings
	status          policyreconcile.ActualState
}

// NewHandler returns a HandlerFactory that creates one HelmHandler per policy ID.
// nsDriftCoordinator may be nil to opt out of namespace-drift reapply (e.g. tests).
func NewHandler(
	kube *kubernetes.KubeClient,
	helm libhelmtypes.HelmPackageManager,
	pc client.PortainerClient,
	coordinator *RestoreCoordinator,
	reporter *ChartStatusReporter,
	nsDriftCoordinator *NamespaceDriftCoordinator,
) policyreconcile.HandlerFactory {
	// kube is a concrete *kubernetes.KubeClient; only assign it to the
	// NamespaceLister interface when non-nil, otherwise namespaceLister would
	// be a non-nil interface wrapping a nil pointer and checkNamespaceDrift's
	// nil check would never trip.
	var namespaceLister NamespaceLister
	if kube != nil {
		namespaceLister = kube
	}

	return func(policyID portainer.PolicyID) policyreconcile.PolicyHandler {
		h := &HelmHandler{
			policyID:           policyID,
			kubeClient:         kube,
			helmPackageManager: helm,
			portainerClient:    pc,
			coordinator:        coordinator,
			chartReporter:      reporter,
			nsDriftCoordinator: nsDriftCoordinator,
			namespaceLister:    namespaceLister,
			installedCharts:    make(map[string]chartRecord),
			lastBundles:        make(map[string]portainer.PolicyChartBundle),
			status: policyreconcile.ActualState{
				PolicyID: policyID,
				Type:     helmPolicyType,
			},
		}
		if nsDriftCoordinator != nil {
			nsDriftCoordinator.register(policyID, h)
		}
		return h
	}
}

func (h *HelmHandler) Apply(ctx context.Context, raw json.RawMessage) error {
	h.setStatus(policyreconcile.ActualState{
		PolicyID: h.policyID,
		Type:     helmPolicyType,
		Status:   policyreconcile.StatusApplying,
	})

	var cfg HelmPolicyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		h.setStatus(policyreconcile.ActualState{
			PolicyID: h.policyID,
			Type:     helmPolicyType,
			Status:   policyreconcile.StatusFailed,
			Message:  err.Error(),
		})
		return err
	}
	h.mu.Lock()
	if cfg.RestoreSettings != nil {
		h.pendingRestore = cfg.RestoreSettings
	}
	h.mu.Unlock()

	fingerprint := libpolicy.HelmPolicyFingerprint(cfg.Charts)

	if err := h.reconcileCharts(ctx, cfg); err != nil {
		h.setStatus(policyreconcile.ActualState{
			PolicyID:    h.policyID,
			Type:        helmPolicyType,
			Fingerprint: fingerprint,
			Status:      policyreconcile.StatusFailed,
			Message:     err.Error(),
		})
		return err
	}

	h.setStatus(policyreconcile.ActualState{
		PolicyID:    h.policyID,
		Type:        helmPolicyType,
		Fingerprint: fingerprint,
		Status:      policyreconcile.StatusApplied,
		Message:     "Successfully applied",
	})
	return nil
}

func (h *HelmHandler) Remove(ctx context.Context) error {
	if h.nsDriftCoordinator != nil {
		h.nsDriftCoordinator.unregister(h.policyID)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = policyreconcile.ActualState{
		PolicyID: h.policyID,
		Type:     helmPolicyType,
		Status:   policyreconcile.StatusRemoving,
	}
	h.markAllChartsUninstalling()
	if h.chartReporter != nil {
		h.chartReporter.Set(h.policyID, buildChartStatuses(0, h.installedCharts))
	}
	h.uninstallCharts()
	if h.chartReporter != nil {
		h.chartReporter.Clear(h.policyID)
	}
	if h.pendingRestore != nil && h.pendingRestore.Manifest != "" {
		h.coordinator.Enqueue(h.policyID, h.pendingRestore.Manifest)
	}
	return nil
}

// Status returns the current policy-level status for this Helm handler.
func (h *HelmHandler) Status() policyreconcile.ActualState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

// ChartsToInstall returns the chart names the coordinator must fetch bundles for.
// A chart is included when its fingerprint differs OR its status is not Installed —
// the status check ensures a chart stuck in a failed/installing state is retried
// even when the server hasn't changed the fingerprint.
// Only returns charts that actually need work; never returns all charts unconditionally.
func (h *HelmHandler) ChartsToInstall(summaries []portainer.PolicyChartSummary) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var toInstall []string
	for _, s := range summaries {
		rec, ok := h.installedCharts[s.ChartName]
		if !ok || rec.Fingerprint != s.Fingerprint || rec.Status != portainer.HelmInstallStatusInstalled {
			toInstall = append(toInstall, s.ChartName)
		}
	}
	return toInstall
}

// ChartStatuses returns per-chart portainer.PolicyChartStatus records for legacy
// per-chart status reporting. Removed when the per-policy status endpoint lands.
func (h *HelmHandler) ChartStatuses(endpointID portainer.EndpointID) []portainer.PolicyChartStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	statuses := make([]portainer.PolicyChartStatus, 0, len(h.installedCharts))
	for _, rec := range h.installedCharts {
		statuses = append(statuses, portainer.PolicyChartStatus{
			EnvironmentID:   endpointID,
			ChartName:       rec.ChartName,
			Fingerprint:     rec.Fingerprint,
			Status:          rec.Status,
			Message:         rec.Message,
			Namespace:       rec.Namespace,
			LastAttemptTime: time.Now().Unix(),
		})
	}
	return statuses
}

// MarkChartsFailed records fetch/preparation failures before Apply receives bundles.
func (h *HelmHandler) MarkChartsFailed(summaries []portainer.PolicyChartSummary, message string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, summary := range summaries {
		if _, ok := h.installedCharts[summary.ChartName]; !ok {
			h.installedCharts[summary.ChartName] = chartRecord{
				ChartName: summary.ChartName,
			}
		}
		h.setChartFailed(summary.ChartName, message, err)
	}
	h.status = policyreconcile.ActualState{
		PolicyID: h.policyID,
		Type:     helmPolicyType,
		Status:   policyreconcile.StatusFailed,
		Message:  chartFailureMessage(message, err),
	}
	if h.chartReporter != nil {
		h.chartReporter.Set(h.policyID, buildChartStatuses(0, h.installedCharts))
	}
}

// reconcileCharts performs per-chart diff against installedCharts, installs/upgrades
// only changed charts, and rolls up the overall policy status (worst-status wins).
func (h *HelmHandler) reconcileCharts(ctx context.Context, cfg HelmPolicyConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Build lookup of incoming chart summaries.
	incomingByName := make(map[string]portainer.PolicyChartSummary, len(cfg.Charts))
	for _, s := range cfg.Charts {
		incomingByName[s.ChartName] = s
	}

	// Ensure tracking entries exist for all incoming charts.
	for _, s := range cfg.Charts {
		if _, ok := h.installedCharts[s.ChartName]; !ok {
			h.installedCharts[s.ChartName] = chartRecord{
				ChartName: s.ChartName,
				Status:    portainer.HelmInstallStatusInstalling,
			}
		}
	}

	// Remove charts that are no longer part of this policy (rare; SecurityK8s is the
	// only multi-chart policy and it always has both charts).
	for name := range h.installedCharts {
		if _, wanted := incomingByName[name]; !wanted {
			rec := h.installedCharts[name]
			h.uninstallChart(releaseNameForRecord(rec), rec.Namespace)
			delete(h.installedCharts, name)
		}
	}

	// Build a lookup of pre-fetched bundles. The coordinator may supply bundles
	// directly (legacy payload path) or the async command may carry them in
	// PolicyStatesCommandPayload. Either way they land here.
	bundlesByName := make(map[string]portainer.PolicyChartBundle, len(cfg.Bundles))
	for _, b := range cfg.Bundles {
		bundlesByName[b.ChartName] = b
	}

	// Find charts that need installing but whose bundle was not pre-fetched.
	// On-demand sync path: Config carries no bundles → fetch via GetCharts,
	// but only for charts whose fingerprint actually changed. Fetching all charts
	// unconditionally would send chart tarballs every poll cycle, which is a
	// bandwidth regression — bundles should only travel when something needs installing.
	var chartsToFetch []string
	for _, summary := range cfg.Charts {
		if _, ok := bundlesByName[summary.ChartName]; ok {
			continue
		}
		rec := h.installedCharts[summary.ChartName]
		if rec.Fingerprint != summary.Fingerprint || rec.Status != portainer.HelmInstallStatusInstalled {
			chartsToFetch = append(chartsToFetch, summary.ChartName)
		}
	}

	if len(chartsToFetch) > 0 {
		fetched, restoreBundle, err := h.portainerClient.GetCharts(chartsToFetch)
		if err != nil {
			h.setAllFailed("Failed to retrieve charts from server", err)
			return fmt.Errorf("failed to retrieve charts from server. Error: %w", err)
		}
		for _, b := range fetched {
			bundlesByName[b.ChartName] = b
		}
		// Update pendingRestore from the returned bundle if not already set from Apply config.
		if h.pendingRestore == nil {
			for _, summary := range cfg.Charts {
				if pt := RestoreTypeForChart(summary.ChartName); pt != "" {
					if rs, ok := restoreBundle[pt]; ok && rs.Manifest != "" {
						rsCopy := rs
						h.pendingRestore = &rsCopy
						break
					}
				}
			}
		}
	}

	tempDir, err := os.MkdirTemp("", "helm-charts-")
	if err != nil {
		h.setAllFailed("Failed to create temporary directory for charts", err)
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(tempDir); removeErr != nil {
			log.Warn().Err(removeErr).Msg("Failed to remove temporary chart directory")
		}
	}()

	var chartErrors []string
	for _, summary := range cfg.Charts {
		rec := h.installedCharts[summary.ChartName]
		bundle, hasBundleForChart := bundlesByName[summary.ChartName]
		if rec.Fingerprint == summary.Fingerprint && rec.Status == portainer.HelmInstallStatusInstalled && !hasBundleForChart {
			// Already at desired fingerprint; no bundle means no install needed.
			continue
		}
		if !hasBundleForChart {
			// Fingerprint changed but still no bundle after on-demand fetch — skip.
			log.Warn().Str("context", "HelmPolicyHandler").Str("chart", summary.ChartName).Msg("Fingerprint changed but no bundle available, will retry next poll")
			continue
		}

		if err := h.installChartBundle(ctx, bundle, tempDir); err != nil {
			chartErrors = append(chartErrors, fmt.Sprintf("%s: %s", bundle.ChartName, err.Error()))
		} else if len(bundle.WaitForCRDs) > 0 {
			h.waitForCRDs(ctx, bundle.WaitForCRDs)
		}
	}

	// Update the legacy chart-status reporter so dual-emit can feed the old endpoint.
	if h.chartReporter != nil {
		h.chartReporter.Set(h.policyID, buildChartStatuses(0, h.installedCharts))
	}

	if len(chartErrors) > 0 {
		return fmt.Errorf("%s", strings.Join(chartErrors, "; "))
	}
	return nil
}

// installChartBundle performs the full install/upgrade lifecycle for a single chart bundle.
func (h *HelmHandler) installChartBundle(ctx context.Context, bundle portainer.PolicyChartBundle, tempDir string) error {
	releaseName := releaseNameForBundle(bundle)

	// Detect conflicts before any cluster mutation: an existing release that Portainer
	// does not manage must not be hijacked or destroyed. A Portainer-managed release in
	// a non-deployed state is cleaned up so a fresh install can proceed.
	conflict, err := h.prepareForInstall(releaseName, bundle.Namespace)
	if err != nil {
		log.Warn().Err(err).Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Msg("Failed to prepare for install, proceeding anyway")
	}
	if conflict {
		h.setChartConflict(bundle.ChartName, bundle.Fingerprint, bundle.Namespace, externalReleaseConflictMessage)
		log.Warn().Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Str("release", releaseName).Msg("Helm release exists but is not managed by Portainer, reporting conflict")
		return fmt.Errorf("release %q already exists and is not managed by Portainer", releaseName)
	}

	if err := h.deleteResourcesBeforeInstall(ctx, &bundle); err != nil {
		log.Warn().Err(err).Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Msg("Deletion warnings occurred, continuing with install")
	}
	if err := h.adoptResourcesBeforeInstall(ctx, &bundle); err != nil {
		log.Warn().Err(err).Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Msg("Adoption warnings occurred, continuing with install")
	}
	if err := h.applyPreReleaseManifest(ctx, &bundle); err != nil {
		h.setChartFailed(bundle.ChartName, "Failed to apply pre-release manifest", err)
		log.Error().Err(err).Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Msg("Failed to apply pre-release manifest")
		return err
	}

	chartData, err := base64.StdEncoding.DecodeString(bundle.EncodedTgz)
	if err != nil {
		h.setChartFailed(bundle.ChartName, "Failed to decode chart data", err)
		return err
	}
	chartPath := filesystem.JoinPaths(tempDir, bundle.ChartName+".tgz")
	if err := os.WriteFile(chartPath, chartData, 0o644); err != nil {
		h.setChartFailed(bundle.ChartName, "Failed to save chart file", err)
		return err
	}

	valuesData, err := base64.StdEncoding.DecodeString(bundle.EncodedValues)
	if err != nil {
		h.setChartFailed(bundle.ChartName, "Failed to decode chart values", err)
		return err
	}
	valuesPath := filesystem.JoinPaths(tempDir, bundle.ChartName+".yaml")
	if err := os.WriteFile(valuesPath, valuesData, 0o644); err != nil {
		h.setChartFailed(bundle.ChartName, "Failed to save values file", err)
		return err
	}

	log.Info().Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Str("release", releaseName).Str("namespace", bundle.Namespace).Msg("Installing/upgrading Helm chart")

	_, err = h.helmPackageManager.Upgrade(options.InstallOptions{
		Name:            releaseName,
		ValuesFile:      valuesPath,
		Chart:           chartPath,
		Namespace:       bundle.Namespace,
		Wait:            !bundle.NoWait,
		TakeOwnership:   true,
		CreateNamespace: true,
		Atomic:          true,
		// Required so templates using the `lookup` function (e.g. namespace-drift-
		// sensitive charts, see C9S-325) get a working discovery client; leaving
		// this nil falls back to Helm's cli.New() path, which silently no-ops
		// `lookup` instead of erroring.
		KubernetesClusterAccess: exec.InClusterKubeAccess(),
		MaxHistory:              maxChartHistory,
	})
	if err != nil {
		h.setChartFailed(bundle.ChartName, "Failed to install/upgrade Helm chart", err)
		log.Error().Err(err).Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Msg("Failed to install/upgrade Helm chart")
		return err
	}

	h.installedCharts[bundle.ChartName] = chartRecord{
		ChartName:   bundle.ChartName,
		ReleaseName: releaseName,
		Fingerprint: bundle.Fingerprint,
		Namespace:   bundle.Namespace,
		Status:      portainer.HelmInstallStatusInstalled,
		Message:     "Successfully installed",
	}

	// Cache the bundle for namespaceDriftSensitiveCharts only, so
	// NamespaceDriftCoordinator can re-run Upgrade() without a server
	// round-trip. Other charts don't need this and it would otherwise hold
	// onto every chart's tarball/values blob for the handler's lifetime.
	if namespaceDriftSensitiveCharts[bundle.ChartName] {
		h.lastBundles[bundle.ChartName] = bundle
	}

	log.Info().Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Str("namespace", bundle.Namespace).Msg("Successfully installed/upgraded Helm chart")
	return nil
}

// waitForCRDs polls until the named CRDs have functioning API endpoints in discovery.
// Checks the actual resource endpoint (not just CRD object existence) because
// gatekeeper may update stub CRDs with real schemas, briefly de-registering them.
func (h *HelmHandler) waitForCRDs(ctx context.Context, crdNames []string) {
	if h.kubeClient == nil {
		return
	}

	const (
		pollInterval = 3 * time.Second
		timeout      = 90 * time.Second
	)

	deadline := time.Now().Add(timeout)
	for {
		allReady := true
		for _, crd := range crdNames {
			if !h.kubeClient.CRDResourceReady(ctx, crd) {
				allReady = false
				break
			}
		}
		if allReady {
			log.Info().Str("context", "HelmPolicyHandler").Int("count", len(crdNames)).Msg("All WaitForCRDs are available in API discovery")
			return
		}
		if time.Now().After(deadline) {
			log.Warn().Str("context", "HelmPolicyHandler").Dur("timeout", timeout).Msg("Timed out waiting for CRDs; proceeding anyway (next chart may fail and retry on next poll)")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// uninstallCharts uninstalls all charts tracked by this handler.
// Called from Remove() when the policy is dropped.
// Always returns nil — uninstall is best-effort; individual chart errors are
// logged but not surfaced. The reconciler discards the handler regardless of
// Remove()'s return value, so propagating partial errors would have no effect.
func (h *HelmHandler) uninstallCharts() {
	for _, rec := range h.installedCharts {
		h.uninstallChart(releaseNameForRecord(rec), rec.Namespace)
	}
}

func (h *HelmHandler) uninstallChart(releaseName, namespace string) {
	log.Info().Str("context", "HelmPolicyHandler").Str("release", releaseName).Msg("Uninstalling Helm chart")
	if err := h.helmPackageManager.Uninstall(options.UninstallOptions{
		Name:      releaseName,
		Namespace: namespace,
		Wait:      true,
	}); err != nil {
		if !isNotFoundError(err) {
			log.Error().Err(err).Str("context", "HelmPolicyHandler").Str("release", releaseName).Msg("Failed to uninstall Helm chart")
		}
	} else {
		log.Info().Str("context", "HelmPolicyHandler").Str("release", releaseName).Msg("Successfully uninstalled Helm chart")
	}
}

// prepareForInstall inspects the existing release history and decides whether the
// install may proceed.
//
// Returns (true, nil) when a release with this name exists that Portainer does not
// manage (no portainer/chart-path annotation) — the caller must report a conflict
// rather than hijack or destroy an externally-managed release.
// Returns (false, nil) for a clean path: no release, a Portainer-managed deployed
// release (upgrade allowed), or a Portainer-managed non-deployed release that was
// cleaned up so a fresh install can proceed.
func (h *HelmHandler) prepareForInstall(releaseName, namespace string) (conflict bool, err error) {
	history, err := h.helmPackageManager.GetHistory(options.HistoryOptions{
		Name:      releaseName,
		Namespace: namespace,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("failed to get release history: %w", err)
	}
	if len(history) == 0 {
		return false, nil
	}
	latest := history[0]
	if latest.Info == nil {
		return false, nil
	}

	if latest.ChartReference.ChartPath == "" {
		// No portainer/chart-path annotation — externally managed, block regardless of status.
		log.Info().Str("context", "HelmPolicyHandler").Str("release", releaseName).Str("namespace", namespace).
			Str("status", string(latest.Info.Status)).Msg("Detected externally-managed release, reporting conflict")
		return true, nil
	}

	if string(latest.Info.Status) == "deployed" {
		// Portainer-managed deployed release — allow upgrade.
		return false, nil
	}

	log.Info().Str("context", "HelmPolicyHandler").Str("release", releaseName).Str("namespace", namespace).
		Str("status", string(latest.Info.Status)).
		Msg("Found Portainer-managed release in non-deployed state, cleaning up before fresh install")

	uninstallOpts := options.UninstallOptions{Name: releaseName, Namespace: namespace, Wait: true}
	if err := h.helmPackageManager.Uninstall(uninstallOpts); err != nil {
		log.Warn().Err(err).Str("context", "HelmPolicyHandler").Str("release", releaseName).
			Msg("Standard uninstall failed, attempting to force-remove release history")
		if forceErr := h.helmPackageManager.ForceRemoveRelease(uninstallOpts); forceErr != nil {
			return false, fmt.Errorf("failed to force-remove release after uninstall failure: %w (original: %w)", forceErr, err)
		}
		log.Info().Str("context", "HelmPolicyHandler").Str("release", releaseName).Msg("Successfully force-removed stuck release history")
		return false, nil
	}
	log.Info().Str("context", "HelmPolicyHandler").Str("release", releaseName).Str("namespace", namespace).Msg("Successfully cleaned up non-deployed release")
	return false, nil
}

func (h *HelmHandler) applyPreReleaseManifest(ctx context.Context, bundle *portainer.PolicyChartBundle) error {
	if bundle.PreReleaseManifest == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(bundle.PreReleaseManifest)
	if err != nil {
		return fmt.Errorf("failed to decode pre-release manifest: %w", err)
	}
	f, err := os.CreateTemp("", "pre-release-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(f.Name()); removeErr != nil {
			log.Warn().Err(removeErr).Msg("failed to remove temporary pre-release manifest file")
		}
	}()
	if _, err := f.Write(decoded); err != nil {
		logs.CloseAndLogErr(f)
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	// Explicit close before Deploy to flush writes; no defer close to avoid double-close.
	logs.CloseAndLogErr(f)

	kubeDeployer := exec.NewKubernetesDeployer(h.kubeClient)
	if err := kubeDeployer.Deploy(ctx, bundle.ChartName, []string{f.Name()}, agentdeployer.DeployOptions{
		DeployerBaseOptions: agentdeployer.DeployerBaseOptions{
			Namespace: bundle.Namespace,
		},
	}); err != nil {
		return fmt.Errorf("failed to apply pre-release manifest: %w", err)
	}
	log.Debug().Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).Str("namespace", bundle.Namespace).Msg("Applied pre-release manifest")
	return nil
}

func (h *HelmHandler) deleteResourcesBeforeInstall(ctx context.Context, bundle *portainer.PolicyChartBundle) error {
	for _, deletion := range bundle.PreInstallDeletions {
		if err := h.kubeClient.DeleteResource(ctx, deletion.APIVersion, deletion.Kind, deletion.Name, deletion.Namespace); err != nil {
			if !isNotFoundError(err) {
				log.Warn().Err(err).Str("kind", deletion.Kind).Str("name", deletion.Name).Msg("failed to delete resource, may not exist")
			}
		}
	}
	return nil
}

func (h *HelmHandler) adoptResourcesBeforeInstall(ctx context.Context, bundle *portainer.PolicyChartBundle) error {
	for _, adoption := range bundle.PreInstallAdoptions {
		if err := h.adoptResource(ctx, bundle.ChartName, bundle.Namespace, adoption); err != nil {
			log.Warn().Err(err).Str("kind", adoption.Kind).Str("name", adoption.Name).Msg("failed to adopt resource, skipping")
		}
	}
	return nil
}

func (h *HelmHandler) adoptResource(ctx context.Context, releaseName, releaseNamespace string, adoption portainer.ResourceAdoption) error {
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{
				"meta.helm.sh/release-name":      releaseName,
				"meta.helm.sh/release-namespace": releaseNamespace,
			},
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "Helm",
			},
		},
	}
	patchData, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal adoption patch: %w", err)
	}

	if _, err := h.kubeClient.GetResource(ctx, adoption.APIVersion, adoption.Kind, adoption.Name, adoption.Namespace); err != nil {
		if isNotFoundError(err) {
			return nil
		}
		// Adoption is best-effort: if we can't verify existence, proceed and
		// let Helm's own conflict detection surface the problem if it matters.
		return nil
	}
	if err := h.kubeClient.PatchResource(ctx, adoption.APIVersion, adoption.Kind, adoption.Name, adoption.Namespace, string(patchData)); err != nil {
		return fmt.Errorf("failed to patch resource: %w", err)
	}
	return nil
}

func (h *HelmHandler) setChartFailed(name, message string, err error) {
	msg := message
	if err != nil {
		msg = fmt.Sprintf("%s: %s", message, err.Error())
	}
	rec := h.installedCharts[name]
	rec.Status = portainer.HelmInstallStatusFailed
	rec.Message = msg
	// Keep the fingerprint so the server can persist the failed status.
	// The next poll retries because status != Installed triggers re-install.
	h.installedCharts[name] = rec
}

// setChartConflict marks a chart as conflicting with an externally-managed Helm release.
// The fingerprint is recorded so the status is meaningful; retry is still driven by
// status != Installed, so the conflict self-heals on the next poll once the user
// removes the conflicting release.
func (h *HelmHandler) setChartConflict(name, fingerprint, namespace, message string) {
	rec := h.installedCharts[name]
	rec.ChartName = name
	rec.Fingerprint = fingerprint
	rec.Namespace = namespace
	rec.Status = portainer.HelmInstallStatusConflict
	rec.Message = message
	h.installedCharts[name] = rec
}

func (h *HelmHandler) setAllFailed(message string, err error) {
	for name := range h.installedCharts {
		h.setChartFailed(name, message, err)
	}
}

func (h *HelmHandler) setStatus(status policyreconcile.ActualState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
}

func chartFailureMessage(message string, err error) string {
	if err == nil {
		return message
	}
	return fmt.Sprintf("%s: %s", message, err.Error())
}

func (h *HelmHandler) markAllChartsUninstalling() {
	for name, rec := range h.installedCharts {
		rec.Status = portainer.HelmInstallStatusUninstalling
		rec.Message = "Uninstalling chart"
		h.installedCharts[name] = rec
	}
}

// releaseNameForBundle returns the Helm release name for a bundle. If ReleaseName is
// set it takes precedence over ChartName, allowing a policy to install under a custom
// name (e.g. "kubernetes-agent" vs the chart name "portainer-observability-k8s").
func releaseNameForBundle(bundle portainer.PolicyChartBundle) string {
	if bundle.ReleaseName != "" {
		return bundle.ReleaseName
	}
	return bundle.ChartName
}

// releaseNameForRecord returns the tracked release name, falling back to the chart name
// for records created before a successful install (which have no ReleaseName yet).
func releaseNameForRecord(rec chartRecord) string {
	if rec.ReleaseName != "" {
		return rec.ReleaseName
	}
	return rec.ChartName
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "NotFound")
}
