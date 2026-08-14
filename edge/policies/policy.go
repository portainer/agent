package policies

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/policies/helm"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/policyreconcile"
	portainer "github.com/portainer/portainer/api"
	libhelmtypes "github.com/portainer/portainer/pkg/libhelm/types"

	"github.com/rs/zerolog/log"
)

// PolicyManager is the thin per-policy coordinator used when the agent receives the
// legacy per-chart payload (PolicyChartSummaries). It groups summaries by PolicyID,
// dispatches to per-policy HelmHandlers, and reports chart-level statuses.
//
// This coordinator is kept alive alongside the generic Reconciler so new agents can
// fall back to the legacy payload path when talking to older servers. It is removed
// when the legacy payload path is deleted.
type PolicyManager struct {
	handlers           map[portainer.PolicyID]*helm.HelmHandler
	factory            policyreconcile.HandlerFactory
	mu                 sync.Mutex
	portainerClient    client.PortainerClient
	kubeClient         *kubernetes.KubeClient
	helmPackageManager libhelmtypes.HelmPackageManager
	coordinator        *helm.RestoreCoordinator
	chartReporter      *helm.ChartStatusReporter
	endpointID         portainer.EndpointID
}

func NewPolicyManager(
	portainerClient client.PortainerClient,
	kubeClient *kubernetes.KubeClient,
	helmPackageManager libhelmtypes.HelmPackageManager,
	coordinator *helm.RestoreCoordinator,
	chartReporter *helm.ChartStatusReporter,
	nsDriftCoordinator *helm.NamespaceDriftCoordinator,
	endpointID portainer.EndpointID,
) *PolicyManager {
	pm := &PolicyManager{
		handlers:           make(map[portainer.PolicyID]*helm.HelmHandler),
		portainerClient:    portainerClient,
		kubeClient:         kubeClient,
		helmPackageManager: helmPackageManager,
		coordinator:        coordinator,
		chartReporter:      chartReporter,
		endpointID:         endpointID,
	}
	pm.factory = helm.NewHandler(kubeClient, helmPackageManager, portainerClient, coordinator, chartReporter, nsDriftCoordinator)
	return pm
}

func (pm *PolicyManager) Reset() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.handlers = make(map[portainer.PolicyID]*helm.HelmHandler)
}

// ProcessPolicyHelmCharts groups the incoming per-chart summaries by PolicyID, fetches
// chart bundles only for charts whose fingerprint changed (on-demand), dispatches
// Apply/Remove to each HelmHandler, and reports per-chart statuses to the server.
//
// A zero PolicyID in a summary means the server is not populating the field.
// Those summaries are logged and skipped — not synthesised — to avoid stale handlers.
func (pm *PolicyManager) ProcessPolicyHelmCharts(summaries []portainer.PolicyChartSummary) {
	if !pm.mu.TryLock() {
		log.Warn().Str("context", "HelmPolicyCoordinator").Msg("Previous ProcessPolicyHelmCharts still running, skipping this cycle")
		return
	}
	defer pm.mu.Unlock()

	// Group summaries by PolicyID; drop entries with zero ID (server not populating the field).
	byPolicy := make(map[portainer.PolicyID][]portainer.PolicyChartSummary)
	for _, s := range summaries {
		if s.PolicyID == 0 {
			log.Warn().Str("context", "HelmPolicyCoordinator").Str("chart", s.ChartName).Msg("PolicyChartSummary has zero PolicyID (server not populating the field), skipping")
			continue
		}
		byPolicy[s.PolicyID] = append(byPolicy[s.PolicyID], s)
	}

	// Determine which charts need fetching (fingerprint changed or not yet installed).
	var allChartsToInstall []string
	chartsToInstallByPolicy := make(map[portainer.PolicyID][]string, len(byPolicy))
	for policyID, policySummaries := range byPolicy {
		handler, err := pm.getOrCreateHandler(policyID)
		if err != nil {
			log.Error().Err(err).Str("context", "HelmPolicyCoordinator").Int("policy_id", int(policyID)).Msg("Failed to create handler for policy")
			continue
		}
		toInstall := handler.ChartsToInstall(policySummaries)
		chartsToInstallByPolicy[policyID] = toInstall
		allChartsToInstall = append(allChartsToInstall, toInstall...)
	}

	// Fetch only the bundles that are needed — never all charts unconditionally.
	// Sending chart tarballs on every poll would be a bandwidth regression; bundles
	// should only travel when a chart's fingerprint has changed and an install is needed.
	var bundlesByName map[string]portainer.PolicyChartBundle
	var restoreBundle portainer.RestoreSettingsBundle
	if len(allChartsToInstall) > 0 {
		bundles, rb, err := pm.portainerClient.GetCharts(allChartsToInstall)
		if err != nil {
			log.Error().Err(err).Str("context", "HelmPolicyCoordinator").Msg("Failed to retrieve charts from server")
			for policyID, chartNames := range chartsToInstallByPolicy {
				if len(chartNames) == 0 {
					continue
				}
				pm.handlers[policyID].MarkChartsFailed(
					filterSummariesByChartName(byPolicy[policyID], chartNames),
					"Failed to retrieve charts from server",
					err,
				)
			}
			pm.reportStatuses()
			return
		}
		bundlesByName = make(map[string]portainer.PolicyChartBundle, len(bundles))
		for _, b := range bundles {
			bundlesByName[b.ChartName] = b
		}
		restoreBundle = rb
	}

	// Apply to each policy's handler.
	for policyID, policySummaries := range byPolicy {
		handler := pm.handlers[policyID]

		toInstall := chartsToInstallByPolicy[policyID]
		var policyBundles []portainer.PolicyChartBundle
		for _, chartName := range toInstall {
			if b, ok := bundlesByName[chartName]; ok {
				policyBundles = append(policyBundles, b)
			}
		}

		// Determine the RestoreSettings for this policy from the returned restoreBundle.
		restoreSettings := restoreSettingsForPolicy(policySummaries, restoreBundle)

		cfg := helm.HelmPolicyConfig{
			Charts:          policySummaries,
			Bundles:         policyBundles,
			RestoreSettings: restoreSettings,
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			log.Error().Err(err).Str("context", "HelmPolicyCoordinator").Int("policy_id", int(policyID)).Msg("Failed to marshal HelmPolicyConfig")
			continue
		}
		if err := handler.Apply(context.Background(), raw); err != nil {
			log.Error().Err(err).Str("context", "HelmPolicyCoordinator").Int("policy_id", int(policyID)).Msg("HelmHandler.Apply failed")
		}
	}

	// Remove handlers for policies no longer in the desired set.
	for policyID, handler := range pm.handlers {
		if _, wanted := byPolicy[policyID]; wanted {
			continue
		}
		if err := handler.Remove(context.Background()); err != nil {
			log.Error().Err(err).Str("context", "HelmPolicyCoordinator").Int("policy_id", int(policyID)).Msg("HelmHandler.Remove failed")
		}
		delete(pm.handlers, policyID)
	}

	pm.reportStatuses()
}

func (pm *PolicyManager) getOrCreateHandler(policyID portainer.PolicyID) (*helm.HelmHandler, error) {
	h, ok := pm.handlers[policyID]
	if !ok {
		handler := pm.factory(policyID)
		h, ok = handler.(*helm.HelmHandler)
		if !ok {
			return nil, fmt.Errorf("factory returned %T, expected *helm.HelmHandler", handler)
		}
		pm.handlers[policyID] = h
	}
	return h, nil
}

// reportStatuses collects per-chart statuses from all live handlers and sends them
// to the server via UpdatePolicyChartStatuses (legacy per-chart endpoint).
func (pm *PolicyManager) reportStatuses() {
	var statuses []portainer.PolicyChartStatus
	for _, handler := range pm.handlers {
		statuses = append(statuses, handler.ChartStatuses(pm.endpointID)...)
	}
	if err := pm.portainerClient.UpdatePolicyChartStatuses(statuses); err != nil {
		log.Error().Err(err).Str("context", "HelmPolicyCoordinator").Msg("Failed to update policy chart statuses on server")
	}
}

// restoreSettingsForPolicy picks the right RestoreSettings from the returned bundle
// by mapping chart names to their policy type.
func restoreSettingsForPolicy(summaries []portainer.PolicyChartSummary, bundle portainer.RestoreSettingsBundle) *portainer.RestoreSettings {
	if len(bundle) == 0 {
		return nil
	}
	for _, s := range summaries {
		if pt := helm.RestoreTypeForChart(s.ChartName); pt != "" {
			if rs, ok := bundle[pt]; ok && rs.Manifest != "" {
				rs := rs // copy to take address
				return &rs
			}
		}
	}
	return nil
}

func filterSummariesByChartName(summaries []portainer.PolicyChartSummary, chartNames []string) []portainer.PolicyChartSummary {
	if len(chartNames) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(chartNames))
	for _, chartName := range chartNames {
		wanted[chartName] = struct{}{}
	}

	filtered := make([]portainer.PolicyChartSummary, 0, len(chartNames))
	for _, summary := range summaries {
		if _, ok := wanted[summary.ChartName]; ok {
			filtered = append(filtered, summary)
		}
	}
	return filtered
}
