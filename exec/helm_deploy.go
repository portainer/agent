package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/portainer/agent/deployer"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/libhelm/options"
	"github.com/portainer/portainer/pkg/libhelm/sdk"
	helmtypes "github.com/portainer/portainer/pkg/libhelm/types"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/rest"
	sigsyaml "sigs.k8s.io/yaml"
)

// HelmDeployer implements the Deployer interface for Helm chart deployments
type HelmDeployer struct {
	helmManager helmtypes.HelmPackageManager
	kubeClient  *kubernetes.KubeClient
}

// NewHelmDeployer creates a new HelmDeployer instance
func NewHelmDeployer(kubeClient *kubernetes.KubeClient) *HelmDeployer {
	return &HelmDeployer{
		helmManager: sdk.NewHelmSDKPackageManager(),
		kubeClient:  kubeClient,
	}
}

// newHelmDeployerWithManager creates a HelmDeployer with a custom manager (for testing)
func newHelmDeployerWithManager(manager helmtypes.HelmPackageManager, kubeClient *kubernetes.KubeClient) *HelmDeployer {
	return &HelmDeployer{helmManager: manager, kubeClient: kubeClient}
}

// Deploy deploys a Helm chart using the Helm SDK
func (d *HelmDeployer) Deploy(ctx context.Context, name string, filePaths []string, deployOpts deployer.DeployOptions) error {
	log.Debug().
		Str("context", "HelmDeployer").
		Str("release_name", name).
		Str("namespace", deployOpts.Namespace).
		Str("working_dir", deployOpts.WorkingDir).
		Msg("deploying Helm chart")

	// Parse Helm configuration from deployOpts
	helmConfig, err := d.parseHelmConfig(deployOpts)
	if err != nil {
		return fmt.Errorf("failed to parse Helm configuration: %w", err)
	}

	if helmConfig.ChartPath == "" && helmConfig.RepoURL == "" {
		return errors.New("either a helm chart path (for git deployments) or a repository URL (for helm repository deployments) is required")
	}

	if helmConfig.ChartPath != "" && helmConfig.RepoURL != "" {
		return errors.New("cannot specify both a helm chart path and a repository URL in the same deployment configuration")
	}

	if helmConfig.RepoURL != "" && helmConfig.ChartName == "" {
		return errors.New("helm chart name is required when using a repository URL")
	}

	releaseName := convertStackNameToReleaseName(name)

	// Parse timeout duration from config or use default
	timeout := 300 * time.Second // Default timeout: 5 minutes
	if helmConfig.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(helmConfig.Timeout)
		if err != nil {
			log.Warn().
				Str("context", "HelmDeployer").
				Str("timeout_value", helmConfig.Timeout).
				Err(err).
				Msg("failed to parse timeout, using default")
		} else {
			timeout = parsedTimeout
		}
	}

	// Prepare install options for Helm SDK
	var installOpts options.InstallOptions
	if helmConfig.ChartPath != "" {
		installOpts, err = d.buildInstallOptsForGitRepo(releaseName, deployOpts, helmConfig, timeout)
	} else {
		installOpts, err = d.buildInstallOptsForHelmRepo(releaseName, deployOpts, helmConfig, timeout)
	}
	if err != nil {
		return err
	}

	// Use helm upgrade --install pattern (idempotent)
	release, err := d.helmManager.Upgrade(installOpts)
	if err != nil {
		return fmt.Errorf("failed to deploy Helm chart: %w", err)
	}

	log.Info().
		Str("context", "HelmDeployer").
		Str("release_name", release.Name).
		Str("release_namespace", release.Namespace).
		Str("requested_namespace", deployOpts.Namespace).
		Str("chart", fmt.Sprintf("%s-%s", release.Chart.Metadata.Name, release.Chart.Metadata.Version)).
		Msg("Helm chart deployed successfully")

	// Log a warning if the release namespace differs from requested namespace
	if release.Namespace != deployOpts.Namespace {
		log.Warn().
			Str("context", "HelmDeployer").
			Str("requested_namespace", deployOpts.Namespace).
			Str("actual_release_namespace", release.Namespace).
			Msg("Helm release namespace differs from requested namespace - check chart templates")
	}

	return nil
}

// buildInstallOptsForGitRepo builds InstallOptions for a git-based deployment.
// Chart files are already on disk after git clone; this resolves paths and merges values files.
func (d *HelmDeployer) buildInstallOptsForGitRepo(releaseName string, deployOpts deployer.DeployOptions, helmConfig *helmConfig, timeout time.Duration) (options.InstallOptions, error) {
	absoluteChartPath, err := resolveChartPath(deployOpts.WorkingDir, helmConfig.ChartPath)
	if err != nil {
		return options.InstallOptions{}, fmt.Errorf("failed to resolve chart path: %w", err)
	}

	log.Debug().
		Str("context", "HelmDeployer").
		Str("relative_chart_path", helmConfig.ChartPath).
		Str("absolute_chart_path", absoluteChartPath).
		Str("working_dir", deployOpts.WorkingDir).
		Msg("resolved chart path")

	absoluteValuesFiles := make([]string, 0, len(helmConfig.ValuesFiles))
	for _, valuesFile := range helmConfig.ValuesFiles {
		if len(valuesFile) == 0 {
			log.Debug().
				Str("context", "HelmDeployer").
				Msg("skipping empty values file entry")
			continue
		}
		absoluteValuesPath, err := resolveChartPath(deployOpts.WorkingDir, valuesFile)
		if err != nil {
			return options.InstallOptions{}, fmt.Errorf("failed to resolve values file path %s: %w", valuesFile, err)
		}
		absoluteValuesFiles = append(absoluteValuesFiles, absoluteValuesPath)
	}

	var mergedValues map[string]any
	for _, valuesFile := range absoluteValuesFiles {
		values, err := sdk.GetHelmValuesFromFile(valuesFile)
		if err != nil {
			return options.InstallOptions{}, fmt.Errorf("failed to read values file %s: %w", valuesFile, err)
		}
		mergedValues = sdk.MergeValues(mergedValues, values)
	}

	return options.InstallOptions{
		Name:                    releaseName,
		Namespace:               deployOpts.Namespace,
		Chart:                   absoluteChartPath,
		Values:                  mergedValues,
		Wait:                    true,
		Timeout:                 timeout,
		CreateNamespace:         true,
		Atomic:                  helmConfig.Atomic,
		KubernetesClusterAccess: d.getKubeAccess(),
		HelmAppLabels:           deployOpts.HelmAppLabels,
	}, nil
}

// buildInstallOptsForHelmRepo builds InstallOptions for a helm-repository-based deployment.
// The chart is fetched directly from the remote repository during install/upgrade.
func (d *HelmDeployer) buildInstallOptsForHelmRepo(releaseName string, deployOpts deployer.DeployOptions, helmConfig *helmConfig, timeout time.Duration) (options.InstallOptions, error) {
	var inlineValues map[string]any
	if helmConfig.ValuesInline != "" {
		if err := sigsyaml.Unmarshal([]byte(helmConfig.ValuesInline), &inlineValues); err != nil {
			return options.InstallOptions{}, fmt.Errorf("failed to parse inline values YAML: %w", err)
		}
	}

	return options.InstallOptions{
		Name:                    releaseName,
		Namespace:               deployOpts.Namespace,
		Repo:                    helmConfig.RepoURL,
		Chart:                   helmConfig.ChartName,
		Version:                 helmConfig.ChartVersion,
		Values:                  inlineValues,
		Wait:                    true,
		Timeout:                 timeout,
		CreateNamespace:         true,
		Atomic:                  helmConfig.Atomic,
		KubernetesClusterAccess: d.getKubeAccess(),
		HelmAppLabels:           deployOpts.HelmAppLabels,
	}, nil
}

// Remove removes a Helm release
func (d *HelmDeployer) Remove(ctx context.Context, name string, filePaths []string, removeOpts deployer.RemoveOptions) error {
	log.Debug().
		Str("context", "HelmDeployer").
		Str("release_name", name).
		Str("namespace", removeOpts.Namespace).
		Msg("uninstalling Helm release")

	releaseName := convertStackNameToReleaseName(name)

	uninstallOpts := options.UninstallOptions{
		Name:                    releaseName,
		Namespace:               removeOpts.Namespace,
		KubernetesClusterAccess: d.getKubeAccess(),
	}

	err := d.helmManager.Uninstall(uninstallOpts)
	if err != nil {
		return fmt.Errorf("failed to uninstall Helm release: %w", err)
	}

	log.Info().
		Str("context", "HelmDeployer").
		Str("release_name", name).
		Str("namespace", removeOpts.Namespace).
		Msg("Helm release uninstalled successfully")

	return nil
}

// Validate validates a Helm chart (currently a no-op, validation happens during deploy)
func (d *HelmDeployer) Validate(ctx context.Context, name string, filePaths []string, validateOpts deployer.ValidateOptions) error {
	log.Debug().
		Str("context", "HelmDeployer").
		Str("release_name", name).
		Msg("validating Helm chart")

	// Helm chart validation happens during the upgrade/install with dry-run
	// For now, we'll just ensure the configuration is parseable
	helmConfig, err := d.parseHelmConfigFromBase(validateOpts.DeployerBaseOptions)
	if err != nil {
		return fmt.Errorf("failed to parse Helm configuration: %w", err)
	}

	if helmConfig.ChartPath == "" && helmConfig.RepoURL == "" {
		return errors.New("either a helm chart path (for git deployments) or a repository URL (for helm repository deployments) is required")
	}

	if helmConfig.ChartPath != "" && helmConfig.RepoURL != "" {
		return errors.New("cannot specify both a helm chart path and a repository URL in the same deployment configuration")
	}

	if helmConfig.RepoURL != "" && helmConfig.ChartName == "" {
		return errors.New("helm chart name is required when using a repository URL")
	}

	log.Debug().
		Str("context", "HelmDeployer").
		Str("chart_path", helmConfig.ChartPath).
		Str("repo_url", helmConfig.RepoURL).
		Int("values_files_count", len(helmConfig.ValuesFiles)).
		Msg("Helm chart configuration validated")

	return nil
}

// Pull is a no-op for Helm as charts are pulled automatically during install/upgrade
func (d *HelmDeployer) Pull(ctx context.Context, name string, filePaths []string, pullOpts deployer.PullOptions) error {
	log.Debug().
		Str("context", "HelmDeployer").
		Str("release_name", name).
		Msg("Helm charts are pulled automatically during deployment")

	return nil
}

// getKubeAccess returns the Kubernetes cluster access configuration
func (d *HelmDeployer) getKubeAccess() *options.KubernetesClusterAccess {
	return InClusterKubeAccess()
}

// InClusterKubeAccess builds an explicit KubernetesClusterAccess for Helm SDK
// operations run from inside the cluster. Leaving KubernetesClusterAccess nil
// makes the SDK fall back to Helm's cli.New()/genericclioptions path, which
// doesn't wire a working discovery client for the SDK's programmatic use (this
// silently breaks the `lookup` template function, see C9S-325) and picks up
// the service account's namespace instead of respecting the namespace
// parameter passed to Helm operations.
func InClusterKubeAccess() *options.KubernetesClusterAccess {
	// Check if running with DEV_KUBECONFIG_PATH (local development)
	devKubeConfigPath := os.Getenv("DEV_KUBECONFIG_PATH")
	if devKubeConfigPath != "" {
		// For local development, return empty struct to use kubeconfig from environment
		return &options.KubernetesClusterAccess{}
	}

	// Get in-cluster config to extract cluster details
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Warn().
			Str("context", "InClusterKubeAccess").
			Err(err).
			Msg("failed to get in-cluster config, falling back to nil (may cause namespace issues)")
		return nil
	}

	// Build KubernetesClusterAccess with in-cluster configuration
	// This ensures Helm respects the namespace parameter instead of using the service account's namespace
	return &options.KubernetesClusterAccess{
		ClusterName:      "in-cluster",
		ContextName:      "in-cluster-context",
		UserName:         "in-cluster-user",
		ClusterServerURL: config.Host,
		AuthToken:        config.BearerToken,
	}
}

// helmConfig holds parsed Helm configuration
type helmConfig struct {
	// Git-based deployment: path to chart inside cloned repository
	ChartPath   string
	ValuesFiles []string
	// Repository-based deployment: fetch chart directly from a Helm repository
	RepoURL      string
	ChartName    string
	ChartVersion string
	ValuesInline string
	// Shared
	Atomic  bool
	Timeout string
}

// parseHelmConfig extracts Helm configuration from DeployOptions
func (d *HelmDeployer) parseHelmConfig(options deployer.DeployOptions) (*helmConfig, error) {
	return d.parseHelmConfigFromBase(options.DeployerBaseOptions)
}

// parseHelmConfigFromBase extracts Helm configuration from environment variables
func (d *HelmDeployer) parseHelmConfigFromBase(baseOpts deployer.DeployerBaseOptions) (*helmConfig, error) {
	config := &helmConfig{
		ValuesFiles: []string{},
	}

	// Parse from environment variables (set by stack manager)
	for _, envVar := range baseOpts.Env {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		switch key {
		case "HELM_CHART_PATH":
			config.ChartPath = value
		case "HELM_VALUES_FILES":
			// Values files are passed as pipe-separated list
			if value != "" {
				config.ValuesFiles = strings.Split(value, "|")
			}
		case "HELM_REPO_URL":
			config.RepoURL = value
		case "HELM_CHART_NAME":
			config.ChartName = value
		case "HELM_CHART_VERSION":
			config.ChartVersion = value
		case "HELM_VALUES_INLINE":
			config.ValuesInline = value
		case "HELM_ATOMIC":
			config.Atomic = value == "true" || value == "1"
		case "HELM_TIMEOUT":
			config.Timeout = value
		}
	}

	log.Debug().
		Str("context", "HelmDeployer").
		Str("chart_path", config.ChartPath).
		Int("values_files_count", len(config.ValuesFiles)).
		Str("repo_url", config.RepoURL).
		Str("chart_name", config.ChartName).
		Str("chart_version", config.ChartVersion).
		Bool("atomic", config.Atomic).
		Str("timeout", config.Timeout).
		Msg("parsed Helm configuration")

	return config, nil
}

// resolveChartPath resolves a relative chart or values file path against workingDir.
// path must be relative; absolute paths are rejected to prevent path traversal.
// workingDir is the edge stack folder (e.g., "/opt/portainer/edge_stacks/1").
func resolveChartPath(workingDir, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("chart path must be relative, got: %s", path)
	}

	absolutePath := filesystem.JoinPaths(workingDir, path)

	if _, err := os.Stat(absolutePath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("chart path does not exist: %s", path)
		}
		return "", fmt.Errorf("failed to resolve chart path: %w", err)
	}

	return absolutePath, nil
}

func convertStackNameToReleaseName(stackName string) string {
	// Use stack name as release name (edge stacks use edge_<stackname> pattern,
	// but Helm naming rule doesn't allow underscores, so we convert to edge-<stackname>)
	if strings.HasPrefix(stackName, "edge_") {
		_, name, ok := strings.Cut(stackName, "edge_")
		if ok {
			return "edge-" + name
		}
	}
	return stackName
}
