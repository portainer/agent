package exec

import (
	"errors"
	"os"
	"testing"

	"github.com/portainer/agent/deployer"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/libhelm/options"
	"github.com/portainer/portainer/pkg/libhelm/release"
	"github.com/portainer/portainer/pkg/libhelm/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHelmManager is a minimal HelmPackageManager for unit tests.
// Only Upgrade and Uninstall are exercised; all other methods are no-ops.
type stubHelmManager struct {
	sdk.HelmSDKPackageManager
	upgradeFunc   func(opts options.InstallOptions) (*release.Release, error)
	uninstallFunc func(opts options.UninstallOptions) error
}

func (m *stubHelmManager) Upgrade(opts options.InstallOptions) (*release.Release, error) {
	if m.upgradeFunc != nil {
		return m.upgradeFunc(opts)
	}
	// Return a release with a non-nil Metadata to avoid a nil-pointer panic: the
	// production success path unconditionally logs release.Chart.Metadata.Name.
	return &release.Release{
		Name:      opts.Name,
		Namespace: opts.Namespace,
		Chart:     release.Chart{Metadata: &release.Metadata{Name: opts.Chart}},
	}, nil
}

func (m *stubHelmManager) Uninstall(opts options.UninstallOptions) error {
	if m.uninstallFunc != nil {
		return m.uninstallFunc(opts)
	}
	return nil
}

// --- convertStackNameToReleaseName ---
func TestConvertStackNameToReleaseName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "edge_ prefix is converted to edge-",
			input:    "edge_myapp",
			expected: "edge-myapp",
		},
		{
			name:     "name without edge_ prefix is returned unchanged",
			input:    "myapp",
			expected: "myapp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, convertStackNameToReleaseName(tc.input))
		})
	}
}

// --- parseHelmConfigFromBase ---

func TestParseHelmConfigFromBase(t *testing.T) {
	t.Parallel()
	d := &HelmDeployer{}

	t.Run("git deployment config", func(t *testing.T) {
		base := deployer.DeployerBaseOptions{
			Env: []string{
				"HELM_CHART_PATH=charts/myapp",
				"HELM_VALUES_FILES=values.yaml|values-prod.yaml",
				"HELM_ATOMIC=true",
				"HELM_TIMEOUT=10m",
			},
		}
		cfg, err := d.parseHelmConfigFromBase(base)
		require.NoError(t, err)
		assert.Equal(t, "charts/myapp", cfg.ChartPath)
		assert.Equal(t, []string{"values.yaml", "values-prod.yaml"}, cfg.ValuesFiles)
		assert.True(t, cfg.Atomic)
		assert.Equal(t, "10m", cfg.Timeout)
		// Repo-only fields must be empty so the deployer routes to the git path.
		assert.Empty(t, cfg.RepoURL)
		assert.Empty(t, cfg.ChartName)
	})

	t.Run("helm repository deployment config", func(t *testing.T) {
		base := deployer.DeployerBaseOptions{
			Env: []string{
				"HELM_REPO_URL=https://charts.example.com",
				"HELM_CHART_NAME=nginx",
				"HELM_CHART_VERSION=15.0.0",
				"HELM_VALUES_INLINE=replicaCount: 2",
			},
		}
		cfg, err := d.parseHelmConfigFromBase(base)
		require.NoError(t, err)
		assert.Equal(t, "https://charts.example.com", cfg.RepoURL)
		assert.Equal(t, "nginx", cfg.ChartName)
		assert.Equal(t, "15.0.0", cfg.ChartVersion)
		assert.Equal(t, "replicaCount: 2", cfg.ValuesInline)
		// Git-only field must be empty so the deployer routes to the repo path.
		assert.Empty(t, cfg.ChartPath)
	})
}

// --- Deploy: validation errors (no manager call needed) ---

func TestDeploy_ValidationErrors(t *testing.T) {
	t.Parallel()
	d := newHelmDeployerWithManager(&stubHelmManager{}, nil)

	tests := []struct {
		name        string
		env         []string
		errContains string
	}{
		{
			name:        "neither chart path nor repo URL",
			env:         []string{},
			errContains: "either a helm chart path",
		},
		{
			name: "both chart path and repo URL",
			env: []string{
				"HELM_CHART_PATH=charts/myapp",
				"HELM_REPO_URL=https://charts.example.com",
			},
			errContains: "cannot specify both",
		},
		{
			name: "repo URL without chart name",
			env: []string{
				"HELM_REPO_URL=https://charts.example.com",
			},
			errContains: "helm chart name is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := d.Deploy(t.Context(), "mystack", nil, deployer.DeployOptions{
				DeployerBaseOptions: deployer.DeployerBaseOptions{
					Namespace: "default",
					Env:       tc.env,
				},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

// --- Deploy: helm repository success ---

func TestDeploy_HelmRepo_Success(t *testing.T) {
	t.Parallel()
	var capturedOpts options.InstallOptions
	mock := &stubHelmManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			capturedOpts = opts
			return &release.Release{
				Name:      opts.Name,
				Namespace: opts.Namespace,
				Chart:     release.Chart{Metadata: &release.Metadata{Name: opts.Chart, Version: "15.0.0"}},
			}, nil
		},
	}

	d := newHelmDeployerWithManager(mock, nil)
	err := d.Deploy(t.Context(), "edge_nginx", nil, deployer.DeployOptions{
		DeployerBaseOptions: deployer.DeployerBaseOptions{
			Namespace: "production",
			Env: []string{
				"HELM_REPO_URL=https://charts.example.com",
				"HELM_CHART_NAME=nginx",
				"HELM_CHART_VERSION=15.0.0",
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "edge-nginx", capturedOpts.Name)
	assert.Equal(t, "production", capturedOpts.Namespace)
	assert.Equal(t, "https://charts.example.com", capturedOpts.Repo)
	assert.Equal(t, "nginx", capturedOpts.Chart)
	assert.Equal(t, "15.0.0", capturedOpts.Version)
}

// --- Deploy: helm manager error propagation ---

func TestDeploy_HelmRepo_ManagerError(t *testing.T) {
	t.Parallel()
	mock := &stubHelmManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return nil, errors.New("cluster unreachable")
		},
	}

	d := newHelmDeployerWithManager(mock, nil)
	err := d.Deploy(t.Context(), "mystack", nil, deployer.DeployOptions{
		DeployerBaseOptions: deployer.DeployerBaseOptions{
			Namespace: "default",
			Env: []string{
				"HELM_REPO_URL=https://charts.example.com",
				"HELM_CHART_NAME=nginx",
			},
		},
	})

	require.Error(t, err)
	// Production code wraps the error; confirm both the wrapper and the root cause are present.
	assert.Contains(t, err.Error(), "failed to deploy Helm chart")
	assert.Contains(t, err.Error(), "cluster unreachable")
}

// --- Remove ---

func TestRemove_Success(t *testing.T) {
	t.Parallel()
	var capturedOpts options.UninstallOptions
	mock := &stubHelmManager{
		uninstallFunc: func(opts options.UninstallOptions) error {
			capturedOpts = opts
			return nil
		},
	}

	d := newHelmDeployerWithManager(mock, nil)
	err := d.Remove(t.Context(), "edge_nginx", nil, deployer.RemoveOptions{
		DeployerBaseOptions: deployer.DeployerBaseOptions{Namespace: "production"},
	})

	require.NoError(t, err)
	assert.Equal(t, "edge-nginx", capturedOpts.Name)
	assert.Equal(t, "production", capturedOpts.Namespace)
}

// --- Deploy: git repository success ---
func TestDeploy_GitRepo_Success(t *testing.T) {
	t.Parallel()
	// Set up a temp working dir that mirrors a cloned git repository:
	//   <workingDir>/charts/myapp/   ← chart directory
	//   <workingDir>/values.yaml     ← values file
	workingDir := t.TempDir()
	chartDir := filesystem.JoinPaths(workingDir, "charts", "myapp")
	require.NoError(t, os.MkdirAll(chartDir, 0o755))
	require.NoError(t, os.WriteFile(
		filesystem.JoinPaths(workingDir, "values.yaml"),
		[]byte("replicaCount: 2\n"),
		0o644,
	))

	var capturedOpts options.InstallOptions
	mock := &stubHelmManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			capturedOpts = opts
			return &release.Release{
				Name:      opts.Name,
				Namespace: opts.Namespace,
				Chart:     release.Chart{Metadata: &release.Metadata{Name: "myapp", Version: "1.0.0"}},
			}, nil
		},
	}

	d := newHelmDeployerWithManager(mock, nil)
	err := d.Deploy(t.Context(), "edge_myapp", nil, deployer.DeployOptions{
		DeployerBaseOptions: deployer.DeployerBaseOptions{
			Namespace:  "staging",
			WorkingDir: workingDir,
			Env: []string{
				"HELM_CHART_PATH=charts/myapp",
				"HELM_VALUES_FILES=values.yaml",
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "edge-myapp", capturedOpts.Name)
	assert.Equal(t, "staging", capturedOpts.Namespace)
	// Chart field should be the resolved absolute path to the chart directory
	assert.Equal(t, chartDir, capturedOpts.Chart)
	// Verify the values file was read and merged. We check key existence rather than
	// the exact integer type because YAML libraries may decode "2" as int, int64, or float64.
	assert.Contains(t, capturedOpts.Values, "replicaCount")
}

func TestResolveChartPath(t *testing.T) {
	t.Run("relative path within workingDir is resolved to absolute path", func(t *testing.T) {
		t.Parallel()
		workingDir := t.TempDir()
		chartDir := filesystem.JoinPaths(workingDir, "charts", "myapp")
		require.NoError(t, os.MkdirAll(chartDir, 0o755))

		got, err := resolveChartPath(workingDir, "charts/myapp")
		require.NoError(t, err)
		assert.Equal(t, chartDir, got)
	})

	t.Run("relative path that does not exist returns error", func(t *testing.T) {
		t.Parallel()
		workingDir := t.TempDir()
		_, err := resolveChartPath(workingDir, "charts/missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart path does not exist")
	})

	t.Run("relative path traversal attempt is rejected by JoinPaths", func(t *testing.T) {
		t.Parallel()
		workingDir := t.TempDir()
		// filesystem.JoinPaths strips the traversal, so the resolved path stays
		// inside workingDir — which does not exist — and stat fails safely.
		_, err := resolveChartPath(workingDir, "../../etc/passwd")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart path does not exist")
	})

	t.Run("absolute path is rejected", func(t *testing.T) {
		t.Parallel()
		workingDir := t.TempDir()
		_, err := resolveChartPath(workingDir, "/etc/passwd")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart path must be relative")
	})

	t.Run("absolute path traversal is rejected", func(t *testing.T) {
		t.Parallel()
		workingDir := t.TempDir()
		// The dangerous HELM_CHART_PATH vector: an absolute path with .. components
		// that would resolve to a sensitive file on the agent node.
		_, err := resolveChartPath(workingDir, "/opt/portainer/edge_stacks/1/../../../../etc/kubernetes/admin.conf")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart path must be relative")
	})

}
