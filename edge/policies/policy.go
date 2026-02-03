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
	policyChartStatus  map[string]*portainer.PolicyChartStatus
	portainerClient    client.PortainerClient
	kubeClient         *kubernetes.KubeClient
	helmPackageManager libhelmtypes.HelmPackageManager
	mu                 sync.Mutex
}

func NewPolicyManager(portainerClient client.PortainerClient, kubeClient *kubernetes.KubeClient, helmPackageManager libhelmtypes.HelmPackageManager) *PolicyManager {
	return &PolicyManager{
		portainerClient:    portainerClient,
		kubeClient:         kubeClient,
		helmPackageManager: helmPackageManager,
		policyChartStatus:  make(map[string]*portainer.PolicyChartStatus),
	}
}

func (pm *PolicyManager) ProcessPolicyHelmCharts(policyChartSummaries []portainer.PolicyChartSummary) {
	if !pm.mu.TryLock() {
		log.Warn().Msg("lock already acquired by previous process policy helm charts")
		return
	}
	defer pm.mu.Unlock()

	chartsToInstall := make([]string, 0, len(policyChartSummaries))
	for _, chart := range policyChartSummaries {
		// Update or Install
		chartStatus, ok := pm.policyChartStatus[chart.ChartName]
		if !ok || chartStatus.Fingerprint != chart.Fingerprint { // if the fingerprint has changed, it means the upstream server has changed the charts, and we need to reinstall/upgrade
			chartsToInstall = append(chartsToInstall, chart.ChartName)

			pm.policyChartStatus[chart.ChartName] = &portainer.PolicyChartStatus{
				ChartName:   chart.ChartName,
				Fingerprint: chart.Fingerprint,
				Status:      portainer.HelmInstallStatusInstalling,
				Namespace:   "", // Namespace will be set when chart is actually installed from chartBundle
			}
		}
	}

	// Get the charts and restore bundle from the server
	chartBundles, restoreBundle, err := pm.portainerClient.GetCharts(chartsToInstall)
	if err != nil {
		pm.setChartsToFailed(chartsToInstall, "Failed to retrieve charts from server")
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

	pm.uninstallRemovedCharts(chartsToUninstall, restoreBundle)

	pm.updatePolicyChartStatuses()
}

func (pm *PolicyManager) installOrUpgradeCharts(policyChartSummaries []string, chartBundles []portainer.PolicyChartBundle) {
	if len(policyChartSummaries) == 0 {
		return
	}

	// Create temporary directory for chart files
	tempDir, err := os.MkdirTemp("", "helm-charts-")
	if err != nil {
		pm.setChartsToFailed(policyChartSummaries, "Failed to create temporary directory for charts")
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
			pm.setChartToFailed(chartBundle.ChartName, "Failed to apply pre-release manifest")
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to apply pre-release manifest")
			continue // Skip chart installation if pre-release manifest fails
		}

		chartData, err := base64.StdEncoding.DecodeString(chartBundle.EncodedTgz)
		if err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to decode chart data")
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to decode chart data")
			continue
		}

		// Save chart to temporary file
		chartPath := filepath.Join(tempDir, chartBundle.ChartName+".tgz")
		if err := os.WriteFile(chartPath, chartData, 0644); err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to save chart file")
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to save chart file")
			continue
		}

		valuesData, err := base64.StdEncoding.DecodeString(chartBundle.EncodedValues)
		if err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to decode chart values")
			log.Error().Err(err).Str("chart", chartBundle.ChartName).Msg("failed to decode chart values")
			continue
		}

		// Save values to a temporary file
		valuesPath := filepath.Join(tempDir, chartBundle.ChartName+".yaml")
		if err := os.WriteFile(valuesPath, valuesData, 0644); err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to save chart file")
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

		log.Info().
			Str("chart", chartBundle.ChartName).
			Str("namespace", chartBundle.Namespace).
			Msg("installing/upgrading Helm chart")

		release, err := pm.helmPackageManager.Upgrade(installOpts)
		if err != nil {
			pm.setChartToFailed(chartBundle.ChartName, "Failed to install/upgrade Helm chart")
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

func (pm *PolicyManager) setChartsToFailed(charts []string, message string) {
	for _, chart := range charts {
		pm.setChartToFailed(chart, message)
	}
}

func (pm *PolicyManager) setChartToFailed(chartName string, message string) {
	if status, ok := pm.policyChartStatus[chartName]; ok {
		status.Status = portainer.HelmInstallStatusFailed
		status.Message = message
		status.LastAttemptTime = time.Now().Unix()
	}
}

func (pm *PolicyManager) setChartToInstalled(chartBundle portainer.PolicyChartBundle, lastAttemptTime int64) {
	if status, ok := pm.policyChartStatus[chartBundle.ChartName]; ok {
		status.Status = portainer.HelmInstallStatusInstalled
		status.Fingerprint = chartBundle.Fingerprint
		status.Namespace = chartBundle.Namespace
		status.LastAttemptTime = lastAttemptTime
	}
}

func (pm *PolicyManager) uninstallRemovedCharts(chartsToUninstall []string, restoreBundle portainer.RestoreSettingsBundle) {
	if len(chartsToUninstall) == 0 {
		return
	}

	for _, chartName := range chartsToUninstall {
		// Set status to uninstalling before attempting uninstall
		if chartStatus, ok := pm.policyChartStatus[chartName]; ok {
			chartStatus.Status = portainer.HelmInstallStatusUninstalling
		}

		log.Info().
			Str("chart", chartName).
			Msg("uninstalling Helm chart removed from policy")

		chartStatus := pm.policyChartStatus[chartName]
		uninstallOpts := options.UninstallOptions{
			Name:      chartName,
			Namespace: chartStatus.Namespace,
		}

		err := pm.helmPackageManager.Uninstall(uninstallOpts)
		if err != nil {
			log.Error().
				Err(err).
				Str("chart", chartName).
				Msg("failed to uninstall Helm chart")

			// Set status back to installed on failure so it can be retried next poll,
			if chartStatus, ok := pm.policyChartStatus[chartName]; ok {
				// only retry if this was not a "not found" error, otherwise it will be retried forever
				if !isNotFoundError(err) {
					chartStatus.Status = portainer.HelmInstallStatusInstalled
				}
			}
			continue
		}

		log.Info().
			Str("chart", chartName).
			Msg("successfully uninstalled Helm chart")

		// Restore environment-level settings after successful uninstall
		if err := pm.restoreEnvironmentSettings(chartName, restoreBundle); err != nil {
			log.Error().
				Err(err).
				Str("chart", chartName).
				Msg("failed to restore environment settings")
		}

		// Remove from our tracking map after successful uninstall
		// Note, we can't guarantee that the chart is fully deleted yet, so here we lose tracking of its status
		delete(pm.policyChartStatus, chartName)
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
	if len(statuses) == 0 {
		return
	}

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
	default:
		return ""
	}
}
