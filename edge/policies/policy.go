package policies

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/portainer/agent/deployer"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/exec"
	"github.com/portainer/agent/kubernetes"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/logs"
	"github.com/portainer/portainer/pkg/libhelm/options"
	libhelmtypes "github.com/portainer/portainer/pkg/libhelm/types"

	"github.com/rs/zerolog/log"
)

type PolicyManager struct {
	policyChartStatus   map[string]*portainer.PolicyChartStatus
	portainerClient     client.PortainerClient
	kubeClient          *kubernetes.KubeClient
	helmPackageManager  libhelmtypes.HelmPackageManager
	mu                  sync.Mutex
	restoredPolicyTypes map[portainer.PolicyType]bool   // tracks which policy types have been restored
	pendingRestorations portainer.RestoreSettingsBundle // stores restoration settings to retry on each poll
}

func NewPolicyManager(portainerClient client.PortainerClient, kubeClient *kubernetes.KubeClient, helmPackageManager libhelmtypes.HelmPackageManager) *PolicyManager {
	return &PolicyManager{
		portainerClient:     portainerClient,
		kubeClient:          kubeClient,
		helmPackageManager:  helmPackageManager,
		policyChartStatus:   make(map[string]*portainer.PolicyChartStatus),
		restoredPolicyTypes: make(map[portainer.PolicyType]bool),
		pendingRestorations: make(portainer.RestoreSettingsBundle),
	}
}

func (pm *PolicyManager) ProcessPolicyHelmCharts(policyChartSummaries []portainer.PolicyChartSummary) {
	if !pm.mu.TryLock() {
		log.Warn().Msg("lock already acquired by previous process policy helm charts")
		return
	}
	defer pm.mu.Unlock()

	// Clear restoredPolicyTypes for any policy type that is back in the bundle (re-attached).
	for _, chart := range policyChartSummaries {
		if policyType := getRestoreTypeForChart(chart.ChartName); policyType != "" {
			delete(pm.restoredPolicyTypes, policyType)
		}
	}

	chartsToInstall := make([]string, 0, len(policyChartSummaries))
	for _, chart := range policyChartSummaries {
		// Update or Install
		chartStatus, ok := pm.policyChartStatus[chart.ChartName]
		if !ok || chartStatus.Fingerprint != chart.Fingerprint { // if the fingerprint has changed, it means the upstream server has changed the charts, and we need to reinstall/upgrade
			chartsToInstall = append(chartsToInstall, chart.ChartName)

			pm.policyChartStatus[chart.ChartName] = &portainer.PolicyChartStatus{
				ChartName:       chart.ChartName,
				Fingerprint:     chart.Fingerprint,
				Status:          portainer.HelmInstallStatusInstalling,
				Message:         "Preparing to install",
				Namespace:       "", // Namespace will be set when chart is actually installed from chartBundle
				LastAttemptTime: time.Now().Unix(),
			}
		}
	}

	// Get the charts and restore bundle from the server
	chartBundles, restoreBundle, err := pm.portainerClient.GetCharts(chartsToInstall)
	if err != nil {
		pm.setChartsToFailedWithError(chartsToInstall, "Failed to retrieve charts from server", err)
		log.Error().Err(err).Msg("failed to retrieve charts from server")

		pm.updatePolicyChartStatuses()

		return
	}

	pm.installOrUpgradeCharts(chartsToInstall, chartBundles)

	currentPolicyChartSet := make(map[string]struct{})
	for _, chart := range policyChartSummaries {
		currentPolicyChartSet[chart.ChartName] = struct{}{}
	}

	chartsToUninstall := make([]string, 0)
	for chartName, chartStatus := range pm.policyChartStatus {
		// if the chart is not in the current policy and not already uninstalling, we need to uninstall it
		_, chartInPolicy := currentPolicyChartSet[chartName]
		if !chartInPolicy && chartStatus.Status != portainer.HelmInstallStatusUninstalling {
			chartsToUninstall = append(chartsToUninstall, chartName)
		}
	}

	pm.uninstallRemovedCharts(chartsToUninstall)

	// If charts were uninstalled, report status immediately and fetch restore bundle
	// This ensures the server knows about the "uninstalling" status and can provide the restore bundle
	if len(chartsToUninstall) > 0 {
		pm.updatePolicyChartStatuses()

		// Fetch restore bundle now that server knows about the uninstalls.
		// Uninstall was called with Wait: true so resources are fully deleted before we apply restore.
		_, restoreBundle, err = pm.portainerClient.GetCharts([]string{})
		if err != nil {
			log.Error().Err(err).Msg("failed to fetch restore bundle after uninstall")
		}
	}

	// Process any pending restorations after uninstalls are complete
	pm.processRestorations(restoreBundle)

	pm.updatePolicyChartStatuses()
}

func (pm *PolicyManager) installOrUpgradeCharts(policyChartSummaries []string, chartBundles []portainer.PolicyChartBundle) {
	if len(policyChartSummaries) == 0 {
		return
	}

	// Create temporary directory for chart files
	tempDir, err := os.MkdirTemp("", "helm-charts-")
	if err != nil {
		pm.setChartsToFailedWithError(policyChartSummaries, "Failed to create temporary directory for charts", err)
		log.Error().Err(err).Msg("failed to create temporary directory for charts")

		return
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			log.Warn().Err(err).Msg("Failed to remove temporary directory for charts")
		}
	}()

	for _, chartBundle := range chartBundles {
		if err := pm.deleteResourcesBeforeInstall(&chartBundle); err != nil {
			log.Warn().
				Err(err).
				Str("chart", chartBundle.ChartName).
				Msg("deletion warnings occurred, continuing with install")
		}

		if err := pm.adoptResourcesBeforeInstall(&chartBundle); err != nil {
			log.Warn().
				Err(err).
				Str("chart", chartBundle.ChartName).
				Msg("adoption warnings occurred, continuing with install")
		}

		if err := pm.applyPreReleaseManifest(&chartBundle); err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to apply pre-release manifest", err)
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to apply pre-release manifest")
			continue // Skip chart installation if pre-release manifest fails
		}

		chartData, err := base64.StdEncoding.DecodeString(chartBundle.EncodedTgz)
		if err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to decode chart data", err)
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to decode chart data")
			continue
		}

		// Save chart to temporary file
		chartPath := filepath.Join(tempDir, chartBundle.ChartName+".tgz")
		if err := os.WriteFile(chartPath, chartData, 0644); err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to save chart file", err)
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to save chart file")
			continue
		}

		valuesData, err := base64.StdEncoding.DecodeString(chartBundle.EncodedValues)
		if err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to decode chart values", err)
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to decode chart values")
			continue
		}

		// Save values to a temporary file
		valuesPath := filepath.Join(tempDir, chartBundle.ChartName+".yaml")
		if err := os.WriteFile(valuesPath, valuesData, 0644); err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to save chart file", err)
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to save chart file")
			continue
		}

		// Install/upgrade chart using Helm SDK
		installOpts := options.InstallOptions{
			Name:            chartBundle.ChartName,
			ValuesFile:      valuesPath,
			Chart:           chartPath,
			Namespace:       chartBundle.Namespace,
			Wait:            true,
			TakeOwnership:   true, // Equivalent to --take-ownership flag
			CreateNamespace: true, // Equivalent to --create-namespace flag
			Atomic:          true, // Equivalent to --atomic flag
		}

		// Check if there's a failed/pending release that needs cleanup before install
		if err := pm.cleanupFailedRelease(chartBundle.ChartName, chartBundle.Namespace); err != nil {
			log.Warn().
				Err(err).
				Str("chart", chartBundle.ChartName).
				Msg("failed to cleanup existing release, proceeding with install anyway")
		}

		log.Info().
			Str("chart", chartBundle.ChartName).
			Str("namespace", chartBundle.Namespace).
			Msg("installing/upgrading Helm chart")

		release, err := pm.helmPackageManager.Upgrade(installOpts)
		if err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to install/upgrade Helm chart", err)
			log.Error().
				Err(err).
				Str("chart", chartBundle.ChartName).
				Msg("failed to install/upgrade Helm chart")
			continue
		}

		lastDeployed := time.Now().Unix()
		if release.Info != nil {
			lastDeployed = release.Info.LastDeployed.Unix()
		}

		pm.setChartToInstalled(chartBundle, lastDeployed)

		log.Info().
			Str("chart", chartBundle.ChartName).
			Str("namespace", chartBundle.Namespace).
			Msg("successfully installed/upgraded Helm chart")
	}
}

// applyPreReleaseManifest applies the pre-release manifest to the Kubernetes cluster
func (pm *PolicyManager) applyPreReleaseManifest(chartBundle *portainer.PolicyChartBundle) error {
	if chartBundle.PreReleaseManifest == "" {
		return nil
	}

	// Create temporary file for the manifest
	tempFile, err := os.CreateTemp("", "pre-release-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			log.Warn().Err(err).Msg("Failed to remove temporary pre-release manifest file")
		}
	}()
	defer logs.CloseAndLogErr(tempFile)

	// Write manifest to file
	decoded, err := base64.StdEncoding.DecodeString(chartBundle.PreReleaseManifest)
	if err != nil {
		return fmt.Errorf("failed to decode pre-release manifest: %w", err)
	}

	if _, err := tempFile.Write(decoded); err != nil {
		return fmt.Errorf("failed to write manifest to temp file: %w", err)
	}
	logs.CloseAndLogErr(tempFile)

	// Create KubernetesDeployer and use Deploy
	kubernetesDeployer := exec.NewKubernetesDeployer(pm.kubeClient)

	// Apply the manifest using KubernetesDeployer
	if err := kubernetesDeployer.Deploy(context.Background(), chartBundle.ChartName, []string{tempFile.Name()}, deployer.DeployOptions{
		DeployerBaseOptions: deployer.DeployerBaseOptions{
			Namespace: chartBundle.Namespace,
		},
	}); err != nil {
		return fmt.Errorf("failed to apply pre-release manifest: %w", err)
	}

	log.Debug().Str("chart", chartBundle.ChartName).Str("namespace", chartBundle.Namespace).Msg("successfully applied pre-release manifest")
	return nil
}

func (pm *PolicyManager) setChartsToFailedWithError(charts []string, message string, err error) {
	for _, chart := range charts {
		pm.setChartToFailed(chart, message, err)
	}
}

func (pm *PolicyManager) setChartToFailed(chartName string, message string, err error) {
	if status, ok := pm.policyChartStatus[chartName]; ok {
		status.Status = portainer.HelmInstallStatusFailed
		// Include the actual error in the message for admin visibility
		if err != nil {
			status.Message = fmt.Sprintf("%s: %s", message, err.Error())
		} else {
			status.Message = message
		}
		// Clear fingerprint so the chart will be retried on next poll
		status.Fingerprint = ""
		status.LastAttemptTime = time.Now().Unix()
	}
}

func (pm *PolicyManager) setChartToInstalled(chartBundle portainer.PolicyChartBundle, lastAttemptTime int64) {
	if status, ok := pm.policyChartStatus[chartBundle.ChartName]; ok {
		status.Status = portainer.HelmInstallStatusInstalled
		status.Fingerprint = chartBundle.Fingerprint
		status.Namespace = chartBundle.Namespace
		status.LastAttemptTime = lastAttemptTime
		status.Message = "Successfully installed"
	}
}

func (pm *PolicyManager) setChartToUninstalling(chartName string) {
	if status, ok := pm.policyChartStatus[chartName]; ok {
		status.Status = portainer.HelmInstallStatusUninstalling
		status.Fingerprint = "" // Clear fingerprint to force reinstall if policy is re-attached
		status.Message = "Uninstalling chart"
		status.LastAttemptTime = time.Now().Unix()
	}
}

func (pm *PolicyManager) setChartUninstallFailed(chartName string, err error) {
	if status, ok := pm.policyChartStatus[chartName]; ok {
		// Set status back to installed so it can be retried next poll
		// unless this was a "not found" error
		if !isNotFoundError(err) {
			status.Status = portainer.HelmInstallStatusInstalled
			status.Message = "Failed to uninstall: " + err.Error()
		} else {
			// Release not found, treat as already uninstalled
			status.Message = "Release not found during uninstall"
		}
		status.LastAttemptTime = time.Now().Unix()
	}
}

func (pm *PolicyManager) uninstallRemovedCharts(chartsToUninstall []string) {
	if len(chartsToUninstall) == 0 {
		return
	}

	for _, chartName := range chartsToUninstall {
		// Set status to uninstalling before attempting uninstall
		pm.setChartToUninstalling(chartName)

		log.Info().
			Str("chart", chartName).
			Msg("uninstalling Helm chart removed from policy")

		chartStatus := pm.policyChartStatus[chartName]
		uninstallOpts := options.UninstallOptions{
			Name:      chartName,
			Namespace: chartStatus.Namespace,
			Wait:      true, // block until resources are deleted so restore manifest is not applied over terminating resources
		}

		err := pm.helmPackageManager.Uninstall(uninstallOpts)
		if err != nil {
			log.Error().
				Err(err).
				Str("chart", chartName).
				Msg("failed to uninstall Helm chart")

			pm.setChartUninstallFailed(chartName, err)
			continue
		}

		log.Info().
			Str("chart", chartName).
			Msg("successfully uninstalled Helm chart")

		// Keep in map with "uninstalling" status - will be reported to server
		// and then deleted after restore bundle is fetched
	}
}

// deleteResourcesBeforeInstall deletes environment-level resources not covered by the policy
// Run this BEFORE adoption and Helm install to clean up orphaned resources
func (pm *PolicyManager) deleteResourcesBeforeInstall(chartBundle *portainer.PolicyChartBundle) error {
	if len(chartBundle.PreInstallDeletions) == 0 {
		return nil
	}

	log.Info().
		Str("context", "PolicyResourceDeletion").
		Int("count", len(chartBundle.PreInstallDeletions)).
		Str("chart", chartBundle.ChartName).
		Msg("Deleting orphaned environment-level resources")

	for _, deletion := range chartBundle.PreInstallDeletions {
		if err := pm.deleteResource(deletion); err != nil {
			log.Warn().
				Str("context", "PolicyResourceDeletion").
				Err(err).
				Str("kind", deletion.Kind).
				Str("name", deletion.Name).
				Str("namespace", deletion.Namespace).
				Msg("Failed to delete resource, may not exist")
		}
	}

	return nil
}

// deleteResource deletes a Kubernetes resource
func (pm *PolicyManager) deleteResource(deletion portainer.ResourceDeletion) error {
	ctx := context.Background()

	err := pm.kubeClient.DeleteResource(ctx, deletion.APIVersion, deletion.Kind, deletion.Name, deletion.Namespace)
	if err != nil {
		if isNotFoundError(err) {
			log.Debug().
				Str("kind", deletion.Kind).
				Str("name", deletion.Name).
				Str("namespace", deletion.Namespace).
				Msg("resource does not exist, nothing to delete")
			return nil
		}
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	log.Debug().
		Str("context", "PolicyResourceDeletion").
		Str("kind", deletion.Kind).
		Str("name", deletion.Name).
		Str("namespace", deletion.Namespace).
		Msg("Successfully deleted orphaned resource")

	return nil
}

// adoptResourcesBeforeInstall adopts existing resources into the Helm release
// Run this BEFORE Helm install to prevent "resource already exists" errors
func (pm *PolicyManager) adoptResourcesBeforeInstall(chartBundle *portainer.PolicyChartBundle) error {
	if len(chartBundle.PreInstallAdoptions) == 0 {
		return nil
	}

	log.Info().
		Str("context", "PolicyResourceAdoption").
		Int("count", len(chartBundle.PreInstallAdoptions)).
		Str("chart", chartBundle.ChartName).
		Msg("Adopting existing resources for Helm release")

	for _, adoption := range chartBundle.PreInstallAdoptions {
		if err := pm.adoptResource(chartBundle.ChartName, chartBundle.Namespace, adoption); err != nil {
			log.Warn().
				Str("context", "PolicyResourceAdoption").
				Err(err).
				Str("kind", adoption.Kind).
				Str("name", adoption.Name).
				Str("namespace", adoption.Namespace).
				Msg("Failed to adopt resource. Resource might not exist, will be skipped")
		}
	}

	return nil
}

// adoptResource transfers ownership of an existing resource to a Helm release via annotations
func (pm *PolicyManager) adoptResource(releaseName, releaseNamespace string, adoption portainer.ResourceAdoption) error {
	// JSON strategic merge patch to add adoption metadata
	patchData := fmt.Sprintf(`{
		"metadata": {
			"annotations": {
				"meta.helm.sh/release-name": "%s",
				"meta.helm.sh/release-namespace": "%s"
			},
			"labels": {
				"app.kubernetes.io/managed-by": "Helm"
			}
		}
	}`, releaseName, releaseNamespace)

	ctx := context.Background()

	// Check if resource exists first (idempotent operation)
	_, err := pm.kubeClient.GetResource(ctx, adoption.APIVersion, adoption.Kind, adoption.Name, adoption.Namespace)
	if err != nil {
		// Resource does not exist, which is expected - Helm will create it
		if isNotFoundError(err) {
			log.Debug().
				Str("kind", adoption.Kind).
				Str("name", adoption.Name).
				Str("namespace", adoption.Namespace).
				Msg("resource does not exist yet, Helm will create it")
			return nil
		}
		// Other errors should be logged but not fail the flow
		log.Warn().
			Err(err).
			Str("kind", adoption.Kind).
			Str("name", adoption.Name).
			Msg("failed to check resource existence")
		return nil
	}

	// Resource exists, patch it to add adoption metadata
	err = pm.kubeClient.PatchResource(ctx, adoption.APIVersion, adoption.Kind, adoption.Name, adoption.Namespace, patchData)
	if err != nil {
		return fmt.Errorf("failed to patch resource with adoption metadata: %w", err)
	}

	log.Debug().
		Str("context", "PolicyResourceAdoption").
		Str("kind", adoption.Kind).
		Str("name", adoption.Name).
		Str("namespace", adoption.Namespace).
		Str("release", releaseName).
		Msg("Successfully adopted resource into Helm release")

	return nil
}

func (pm *PolicyManager) updatePolicyChartStatuses() {
	statuses := make([]portainer.PolicyChartStatus, 0, len(pm.policyChartStatus))
	for _, chartStatus := range pm.policyChartStatus {
		statuses = append(statuses, *chartStatus)
	}

	// Always call UpdatePolicyChartStatuses, even with empty list.
	// This allows the server to clean up stale chart statuses (e.g., "uninstalling")
	// when the agent no longer tracks those charts.
	if err := pm.portainerClient.UpdatePolicyChartStatuses(statuses); err != nil {
		log.Error().Err(err).Msg("failed to update policy chart statuses on server")
	}
}

// isNotFoundError checks if an error is a "not found" error
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common "not found" error indicators
	errMsg := err.Error()
	return strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "NotFound")
}

// restoreEnvironmentSettings restores environment-level settings after a chart is uninstalled
func (pm *PolicyManager) restoreEnvironmentSettings(chartName string, restoreBundle portainer.RestoreSettingsBundle) error {
	restoreType := getRestoreTypeForChart(chartName)
	if restoreType == "" {
		return nil
	}

	restoreSettings, exists := restoreBundle[restoreType]
	if !exists || restoreSettings.Manifest == "" {
		log.Debug().
			Str("chart", chartName).
			Str("restore_type", string(restoreType)).
			Msg("no restore settings")
		return nil
	}

	log.Info().
		Str("chart", chartName).
		Str("restore_type", string(restoreType)).
		Msg("restoring environment settings")

	// Decode the base64-encoded manifest
	manifestBytes, err := base64.StdEncoding.DecodeString(restoreSettings.Manifest)
	if err != nil {
		return fmt.Errorf("failed to decode manifest: %w", err)
	}

	tempFile, err := os.CreateTemp("", fmt.Sprintf("restore-%s-*.yaml", restoreType))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			log.Warn().Err(err).Msg("failed to remove temporary file")
		}
	}()
	defer logs.CloseAndLogErr(tempFile)

	if _, err := tempFile.Write(manifestBytes); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	kubernetesDeployer := exec.NewKubernetesDeployer(pm.kubeClient)
	err = kubernetesDeployer.Deploy(
		context.Background(),
		fmt.Sprintf("restore-%s", restoreType),
		[]string{tempFile.Name()},
		deployer.DeployOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to apply manifest: %w", err)
	}

	log.Info().
		Str("chart", chartName).
		Str("restore_type", string(restoreType)).
		Msg("restored environment settings")
	return nil
}

// getRestoreTypeForChart determines the restore type based on the chart name
func getRestoreTypeForChart(chartName string) portainer.PolicyType {
	switch chartName {
	case "portainer-registry-k8s":
		return portainer.RegistryK8s
	case "gatekeeper", "portainer-security-k8s":
		return portainer.SecurityK8s
	default:
		return ""
	}
}

// cleanupFailedRelease checks if there's an existing release that is NOT in a deployed
// state and removes it to allow a fresh install. This handles cases where a previous
// install/upgrade/rollback attempt failed or left the release stuck in a non-deployed
// state (e.g. failed, pending-*, uninstalling, superseded).
// Helm's "upgrade" command requires at least one deployed release to upgrade from,
// so any non-deployed release must be cleaned up before a fresh install can proceed.
func (pm *PolicyManager) cleanupFailedRelease(releaseName, namespace string) error {
	// Use GetHistory for reliable detection - it directly queries release storage
	// and returns all versions regardless of status, unlike List which may filter.
	historyOpts := options.HistoryOptions{
		Name:      releaseName,
		Namespace: namespace,
	}

	history, err := pm.helmPackageManager.GetHistory(historyOpts)
	if err != nil {
		// If release not found, nothing to clean up
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to get release history: %w", err)
	}

	if len(history) == 0 {
		return nil
	}

	// History is sorted latest first - check if the latest version is deployed
	latestRelease := history[0]
	if latestRelease.Info == nil {
		return nil
	}

	latestStatus := string(latestRelease.Info.Status)
	if latestStatus == "deployed" {
		// Release is healthy, no cleanup needed
		return nil
	}

	log.Info().
		Str("chart", releaseName).
		Str("namespace", namespace).
		Str("status", latestStatus).
		Msg("found release in non-deployed state, cleaning up before fresh install")

	uninstallOpts := options.UninstallOptions{
		Name:      releaseName,
		Namespace: namespace,
		Wait:      true,
	}

	if err := pm.helmPackageManager.Uninstall(uninstallOpts); err != nil {
		log.Warn().
			Err(err).
			Str("chart", releaseName).
			Msg("standard uninstall failed, attempting to force-remove release history")

		// When CRDs are missing, Helm can't build kubernetes objects for deletion and
		// returns early without purging the release history. Force-remove the release
		// so a fresh install can proceed on the next attempt.
		if forceErr := pm.helmPackageManager.ForceRemoveRelease(uninstallOpts); forceErr != nil {
			return fmt.Errorf("failed to force-remove release after uninstall failure: %w (original: %w)", forceErr, err)
		}

		log.Info().
			Str("chart", releaseName).
			Msg("successfully force-removed stuck release history")

		return nil
	}

	log.Info().
		Str("chart", releaseName).
		Str("namespace", namespace).
		Msg("successfully cleaned up non-deployed release")

	return nil
}

// isFailedOrPendingStatus checks if a release status indicates it needs cleanup.
// Kept for backward compatibility in tests, but cleanupFailedRelease now uses
// a broader "is NOT deployed" check via GetHistory.
func isFailedOrPendingStatus(status string) bool {
	switch status {
	case "failed", "pending-install", "pending-upgrade", "pending-rollback":
		return true
	default:
		return false
	}
}

// processRestorations processes any pending environment restorations after uninstalls are complete.
// It stores restoration settings and retries on each poll until successful.
func (pm *PolicyManager) processRestorations(restoreBundle portainer.RestoreSettingsBundle) {
	// Merge incoming restore bundle into pendingRestorations
	// This ensures we keep trying even after the server stops sending the data
	for policyType, restoreSettings := range restoreBundle {
		if restoreSettings.Manifest != "" && !pm.restoredPolicyTypes[policyType] {
			// Only store if not already restored and has valid manifest
			if _, exists := pm.pendingRestorations[policyType]; !exists {
				log.Info().
					Str("policy_type", string(policyType)).
					Msg("storing restoration settings for retry")
				pm.pendingRestorations[policyType] = restoreSettings
			}
		}
	}

	if len(pm.pendingRestorations) == 0 {
		// Clean up any charts that were uninstalled (no restoration needed or already done)
		pm.cleanupUninstalledCharts()
		return
	}

	// Process each pending restoration
	for policyType, restoreSettings := range pm.pendingRestorations {
		// Skip if nothing to restore
		if restoreSettings.Manifest == "" {
			delete(pm.pendingRestorations, policyType)
			continue
		}

		// Check if we've already restored this policy type
		if pm.restoredPolicyTypes[policyType] {
			log.Debug().
				Str("policy_type", string(policyType)).
				Msg("restoration already completed for this policy type, removing from pending")
			delete(pm.pendingRestorations, policyType)
			continue
		}

		log.Info().
			Str("policy_type", string(policyType)).
			Msg("processing environment restoration (retry on each poll until success)")

		// Decode and apply the manifest using kubectl apply
		manifestBytes, err := base64.StdEncoding.DecodeString(restoreSettings.Manifest)
		if err != nil {
			log.Error().
				Err(err).
				Str("policy_type", string(policyType)).
				Msg("failed to decode restoration manifest, will retry on next poll")
			continue
		}

		tempFile, err := os.CreateTemp("", fmt.Sprintf("restore-%s-*.yaml", policyType))
		if err != nil {
			log.Error().
				Err(err).
				Str("policy_type", string(policyType)).
				Msg("failed to create temp file for restoration, will retry on next poll")
			continue
		}

		// Write manifest to temp file
		if _, err := tempFile.Write(manifestBytes); err != nil {
			logs.CloseAndLogErr(tempFile)
			if removeErr := os.Remove(tempFile.Name()); removeErr != nil {
				log.Warn().Err(removeErr).Msg("failed to remove temporary restoration file")
			}
			log.Error().
				Err(err).
				Str("policy_type", string(policyType)).
				Msg("failed to write restoration manifest, will retry on next poll")
			continue
		}
		logs.CloseAndLogErr(tempFile)

		// Apply the manifest using kubectl
		kubernetesDeployer := exec.NewKubernetesDeployer(pm.kubeClient)
		err = kubernetesDeployer.Deploy(
			context.Background(),
			fmt.Sprintf("restore-%s", policyType),
			[]string{tempFile.Name()},
			deployer.DeployOptions{},
		)

		// Cleanup temp file
		if removeErr := os.Remove(tempFile.Name()); removeErr != nil {
			log.Warn().Err(removeErr).Msg("failed to remove temporary restoration file")
		}

		if err != nil {
			log.Error().
				Err(err).
				Str("policy_type", string(policyType)).
				Msg("failed to apply restoration manifest, will retry on next poll")
			continue
		}

		log.Info().
			Str("policy_type", string(policyType)).
			Msg("successfully restored environment settings")

		// Mark this policy type as restored and remove from pending
		pm.restoredPolicyTypes[policyType] = true
		delete(pm.pendingRestorations, policyType)
	}

	// Clean up any charts that have been restored
	pm.cleanupUninstalledCharts()
}

// cleanupUninstalledCharts removes charts with "uninstalling" status from the tracking map
// Called after restoration is complete or when no restoration is needed
func (pm *PolicyManager) cleanupUninstalledCharts() {
	for chartName, status := range pm.policyChartStatus {
		if status.Status == portainer.HelmInstallStatusUninstalling {
			log.Debug().
				Str("chart", chartName).
				Msg("removing uninstalled chart from tracking")
			delete(pm.policyChartStatus, chartName)
		}
	}
}
