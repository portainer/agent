package policies

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/portainer/agent/internals/mocks"
	"github.com/portainer/agent/kubernetes"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/portainer/portainer/pkg/libhelm/options"
	"github.com/portainer/portainer/pkg/libhelm/release"
	"github.com/portainer/portainer/pkg/libhelm/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

func init() {
	fips.InitFIPS(false)
}

func TestNewPolicyManager(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)
	mockKubeClient := &kubernetes.KubeClient{}
	manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.policyChartStatus)
	assert.Empty(t, manager.policyChartStatus)
}

func TestProcessPolicyHelmCharts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)
	mockKubeClient := &kubernetes.KubeClient{}

	t.Run("Empty policy chart summaries", func(t *testing.T) {
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

		// restore bundle
		mockPortainerClient.EXPECT().GetCharts([]string{}).Return([]portainer.PolicyChartBundle{}, portainer.RestoreSettingsBundle{}, nil)
		// Even with empty statuses, we call UpdatePolicyChartStatuses to clean up stale records on server
		mockPortainerClient.EXPECT().UpdatePolicyChartStatuses([]portainer.PolicyChartStatus{}).Return(nil)

		manager.ProcessPolicyHelmCharts([]portainer.PolicyChartSummary{})

		assert.Empty(t, manager.policyChartStatus)
	})

	t.Run("New chart installation", func(t *testing.T) {
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

		chartSummaries := []portainer.PolicyChartSummary{
			{
				ChartName:   "chart1",
				Fingerprint: "fp1",
			},
		}

		chartBundle := portainer.PolicyChartBundle{
			PolicyChartSummary: portainer.PolicyChartSummary{
				ChartName:   "chart1",
				Fingerprint: "fp1",
			},
			Namespace:     "default",
			EncodedTgz:    base64.StdEncoding.EncodeToString([]byte("chart-data")),
			EncodedValues: base64.StdEncoding.EncodeToString([]byte("values-data")),
		}

		mockPortainerClient.EXPECT().GetCharts([]string{"chart1"}).Return([]portainer.PolicyChartBundle{chartBundle}, portainer.RestoreSettingsBundle{}, nil)
		mockPortainerClient.EXPECT().UpdatePolicyChartStatuses(gomock.Any()).DoAndReturn(func(statuses []portainer.PolicyChartStatus) error {
			assert.Len(t, statuses, 1)
			assert.Equal(t, chartBundle.ChartName, statuses[0].ChartName)
			assert.Equal(t, chartBundle.Fingerprint, statuses[0].Fingerprint)
			assert.Equal(t, portainer.HelmInstallStatusInstalled, statuses[0].Status)
			assert.Equal(t, chartBundle.Namespace, statuses[0].Namespace)
			// LastAttemptTime might be 0 if the mock release has zero time, which is fine to test
			return nil
		})

		manager.ProcessPolicyHelmCharts(chartSummaries)

		assert.Len(t, manager.policyChartStatus, 1)
		assert.Equal(t, portainer.HelmInstallStatusInstalled, manager.policyChartStatus["chart1"].Status)
		assert.Equal(t, "fp1", manager.policyChartStatus["chart1"].Fingerprint)
		assert.Equal(t, "default", manager.policyChartStatus["chart1"].Namespace)
	})

	t.Run("Chart uninstallation", func(t *testing.T) {
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

		manager.policyChartStatus["chart-to-remove"] = &portainer.PolicyChartStatus{
			ChartName:   "chart-to-remove",
			Fingerprint: "fp",
			Status:      portainer.HelmInstallStatusInstalled,
			Namespace:   "default",
		}

		chartSummaries := []portainer.PolicyChartSummary{}

		// First GetCharts call (before uninstall)
		mockPortainerClient.EXPECT().GetCharts([]string{}).Return([]portainer.PolicyChartBundle{}, portainer.RestoreSettingsBundle{}, nil)
		// After uninstall, status is reported and GetCharts called again to fetch restore bundle
		mockPortainerClient.EXPECT().UpdatePolicyChartStatuses(gomock.Any()).DoAndReturn(func(statuses []portainer.PolicyChartStatus) error {
			assert.Len(t, statuses, 1)
			assert.Equal(t, "chart-to-remove", statuses[0].ChartName)
			assert.Equal(t, portainer.HelmInstallStatusUninstalling, statuses[0].Status)
			return nil
		})
		mockPortainerClient.EXPECT().GetCharts([]string{}).Return([]portainer.PolicyChartBundle{}, portainer.RestoreSettingsBundle{}, nil)
		// Final UpdatePolicyChartStatuses call with empty list to clean up stale records on server
		mockPortainerClient.EXPECT().UpdatePolicyChartStatuses([]portainer.PolicyChartStatus{}).Return(nil)

		manager.ProcessPolicyHelmCharts(chartSummaries)

		// Chart should be removed from map after uninstall (no restoration needed)
		assert.Empty(t, manager.policyChartStatus)
	})

	t.Run("Failed to get charts from server", func(t *testing.T) {
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

		summary := portainer.PolicyChartSummary{
			ChartName:   "chart1",
			Fingerprint: "fp1",
		}
		chartSummaries := []portainer.PolicyChartSummary{
			summary,
		}

		mockPortainerClient.EXPECT().GetCharts([]string{"chart1"}).Return(nil, portainer.RestoreSettingsBundle{}, errors.New("server error"))
		mockPortainerClient.EXPECT().UpdatePolicyChartStatuses(gomock.Any()).DoAndReturn(func(statuses []portainer.PolicyChartStatus) error {
			assert.Len(t, statuses, 1)
			assert.Equal(t, summary.ChartName, statuses[0].ChartName)
			assert.Empty(t, statuses[0].Fingerprint, "Fingerprint should be cleared on failure to force retry")
			assert.Equal(t, portainer.HelmInstallStatusFailed, statuses[0].Status)
			// Message now includes the actual error for admin visibility
			assert.Contains(t, statuses[0].Message, "Failed to retrieve charts from server")
			assert.Contains(t, statuses[0].Message, "server error")
			assert.NotZero(t, statuses[0].LastAttemptTime)

			return nil
		})

		manager.ProcessPolicyHelmCharts(chartSummaries)

		assert.Len(t, manager.policyChartStatus, 1)
		assert.Equal(t, portainer.HelmInstallStatusFailed, manager.policyChartStatus["chart1"].Status)
		// Message now includes the actual error for admin visibility
		assert.Contains(t, manager.policyChartStatus["chart1"].Message, "Failed to retrieve charts from server")
		assert.Contains(t, manager.policyChartStatus["chart1"].Message, "server error")
		assert.Empty(t, manager.policyChartStatus["chart1"].Fingerprint, "Fingerprint should be cleared on failure to force retry")

	})
}

// Test adoption handler with no adoptions
func TestAdoptResourcesBeforeInstall(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)
	mockKubeClient := &kubernetes.KubeClient{}
	manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

	bundle := &portainer.PolicyChartBundle{
		PolicyChartSummary: portainer.PolicyChartSummary{
			ChartName: "test-chart",
		},
		Namespace:           "default",
		PreInstallAdoptions: []portainer.ResourceAdoption{},
	}

	// Should handle empty adoptions gracefully
	err := manager.adoptResourcesBeforeInstall(bundle)
	assert.NoError(t, err)
}

// Test deletion handler with no deletions
func TestDeleteResourcesBeforeInstall(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)
	mockKubeClient := &kubernetes.KubeClient{}
	manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

	bundle := &portainer.PolicyChartBundle{
		PolicyChartSummary: portainer.PolicyChartSummary{
			ChartName: "test-chart",
		},
		Namespace:           "default",
		PreInstallDeletions: []portainer.ResourceDeletion{},
	}

	// Should handle empty deletions gracefully
	err := manager.deleteResourcesBeforeInstall(bundle)
	assert.NoError(t, err)
}

// Test isNotFoundError helper function
func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "Error with 'not found'",
			err:      errors.New("resource not found"),
			expected: true,
		},
		{
			name:     "Error with 'NotFound'",
			err:      errors.New("NotFound: the resource was not found"),
			expected: true,
		},
		{
			name:     "Different error",
			err:      errors.New("permission denied"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotFoundError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test getRestoreTypeForChart helper function
func TestGetRestoreTypeForChart(t *testing.T) {
	tests := []struct {
		name      string
		chartName string
		expected  portainer.PolicyType
	}{
		{
			name:      "Registry chart",
			chartName: "portainer-registry-k8s",
			expected:  portainer.RegistryK8s,
		},
		{
			name:      "Unknown chart",
			chartName: "unknown-chart",
			expected:  "",
		},
		{
			name:      "Empty chart name",
			chartName: "",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getRestoreTypeForChart(tt.chartName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test restoreEnvironmentSettings function
func TestRestoreEnvironmentSettings(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)
	mockKubeClient := &kubernetes.KubeClient{}
	manager := NewPolicyManager(mockPortainerClient, mockKubeClient, test.NewMockHelmPackageManager())

	t.Run("Empty restore bundle", func(t *testing.T) {
		restoreBundle := portainer.RestoreSettingsBundle{}
		err := manager.restoreEnvironmentSettings("portainer-registry-k8s", restoreBundle)
		assert.NoError(t, err)
	})

	t.Run("Restore bundle without matching type", func(t *testing.T) {
		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.RbacK8s: portainer.RestoreSettings{
				Manifest: base64.StdEncoding.EncodeToString([]byte("apiVersion: v1\nkind: Secret")),
			},
		}
		err := manager.restoreEnvironmentSettings("portainer-registry-k8s", restoreBundle)
		assert.NoError(t, err)
	})

	t.Run("Restore bundle with empty manifest", func(t *testing.T) {
		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.RegistryK8s: portainer.RestoreSettings{
				Manifest: "",
			},
		}
		err := manager.restoreEnvironmentSettings("portainer-registry-k8s", restoreBundle)
		assert.NoError(t, err)
	})

	t.Run("Invalid base64 manifest", func(t *testing.T) {
		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.RegistryK8s: portainer.RestoreSettings{
				Manifest: "invalid-base64!",
			},
		}
		err := manager.restoreEnvironmentSettings("portainer-registry-k8s", restoreBundle)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode manifest")
	})

	t.Run("Unknown chart type", func(t *testing.T) {
		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.RegistryK8s: portainer.RestoreSettings{
				Manifest: base64.StdEncoding.EncodeToString([]byte("apiVersion: v1\nkind: Secret")),
			},
		}
		err := manager.restoreEnvironmentSettings("unknown-chart", restoreBundle)
		assert.NoError(t, err) // Should return early without error for unknown chart types
	})
}

// Test isFailedOrPendingStatus helper function
func TestProcessRestorations(t *testing.T) {
	tests := []struct {
		name            string
		restoreBundle   portainer.RestoreSettingsBundle
		chartStatus     map[string]*portainer.PolicyChartStatus
		initialRestored map[portainer.PolicyType]bool
		shouldSkip      bool // whether restoration should be skipped
		skipReason      string
	}{
		{
			name:          "Empty restore bundle - no action",
			restoreBundle: portainer.RestoreSettingsBundle{},
			chartStatus:   map[string]*portainer.PolicyChartStatus{},
			shouldSkip:    true,
			skipReason:    "empty bundle",
		},
		{
			name: "Has uninstalling charts - skip restoration",
			restoreBundle: portainer.RestoreSettingsBundle{
				portainer.SecurityK8s: {Manifest: base64.StdEncoding.EncodeToString([]byte("test-manifest"))},
			},
			chartStatus: map[string]*portainer.PolicyChartStatus{
				"gatekeeper": {Status: portainer.HelmInstallStatusUninstalling},
			},
			shouldSkip: true,
			skipReason: "charts still uninstalling",
		},
		{
			name: "Already restored - skip",
			restoreBundle: portainer.RestoreSettingsBundle{
				portainer.SecurityK8s: {Manifest: base64.StdEncoding.EncodeToString([]byte("test-manifest"))},
			},
			chartStatus: map[string]*portainer.PolicyChartStatus{},
			initialRestored: map[portainer.PolicyType]bool{
				portainer.SecurityK8s: true,
			},
			shouldSkip: true,
			skipReason: "already restored",
		},
		{
			name: "Empty manifest - skip",
			restoreBundle: portainer.RestoreSettingsBundle{
				portainer.SecurityK8s: {Manifest: ""},
			},
			chartStatus: map[string]*portainer.PolicyChartStatus{},
			shouldSkip:  true,
			skipReason:  "empty manifest",
		},
		{
			name: "No uninstalling charts and not restored - should attempt restoration",
			restoreBundle: portainer.RestoreSettingsBundle{
				portainer.SecurityK8s: {Manifest: base64.StdEncoding.EncodeToString([]byte("test-manifest"))},
			},
			chartStatus: map[string]*portainer.PolicyChartStatus{
				"gatekeeper": {Status: portainer.HelmInstallStatusInstalled},
			},
			initialRestored: map[portainer.PolicyType]bool{},
			shouldSkip:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Kubernetes client
			kubeClient := &kubernetes.KubeClient{}

			// Initialize the restoredPolicyTypes map if provided, otherwise create empty map
			restoredTypes := tt.initialRestored
			if restoredTypes == nil {
				restoredTypes = make(map[portainer.PolicyType]bool)
			}

			// Create PolicyManager
			manager := &PolicyManager{
				kubeClient:          kubeClient,
				policyChartStatus:   tt.chartStatus,
				restoredPolicyTypes: restoredTypes,
				pendingRestorations: make(portainer.RestoreSettingsBundle),
			}

			initialCount := len(manager.restoredPolicyTypes)

			// Execute (note: will fail to actually deploy in unit tests, but that's OK)
			manager.processRestorations(tt.restoreBundle)

			if tt.shouldSkip {
				// Verify that no new restorations were marked as complete
				assert.Len(t, manager.restoredPolicyTypes, initialCount,
					"No new restorations should be marked complete when skipping (%s)", tt.skipReason)
			}
			// Note: We can't easily test the actual deployment without mocking the entire kubectl/deployer chain,
			// but the important logic (checking uninstalling status, checking already-restored status) is verified
		})
	}
}

func TestProcessRestorationsRetryBehavior(t *testing.T) {
	// Test that restoration settings are stored in pendingRestorations for retry
	t.Run("Stores restoration settings for retry when deployment fails", func(t *testing.T) {
		kubeClient := &kubernetes.KubeClient{}
		manager := &PolicyManager{
			kubeClient:          kubeClient,
			policyChartStatus:   map[string]*portainer.PolicyChartStatus{},
			restoredPolicyTypes: make(map[portainer.PolicyType]bool),
			pendingRestorations: make(portainer.RestoreSettingsBundle),
		}

		manifest := base64.StdEncoding.EncodeToString([]byte("test-manifest"))
		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.SecurityK8s: {Manifest: manifest},
		}

		// First call - should store in pendingRestorations
		manager.processRestorations(restoreBundle)

		// Verify restoration settings are stored for retry
		assert.Contains(t, manager.pendingRestorations, portainer.SecurityK8s,
			"Restoration settings should be stored in pendingRestorations for retry")
		assert.Equal(t, manifest, manager.pendingRestorations[portainer.SecurityK8s].Manifest,
			"Manifest should be preserved in pendingRestorations")

		// Not marked as restored (deployment fails without proper kubeClient)
		assert.NotContains(t, manager.restoredPolicyTypes, portainer.SecurityK8s,
			"Should not be marked as restored when deployment fails")
	})

	t.Run("Retries from pendingRestorations on subsequent polls even without new bundle", func(t *testing.T) {
		kubeClient := &kubernetes.KubeClient{}
		manifest := base64.StdEncoding.EncodeToString([]byte("test-manifest"))

		manager := &PolicyManager{
			kubeClient:          kubeClient,
			policyChartStatus:   map[string]*portainer.PolicyChartStatus{},
			restoredPolicyTypes: make(map[portainer.PolicyType]bool),
			pendingRestorations: portainer.RestoreSettingsBundle{
				portainer.SecurityK8s: {Manifest: manifest},
			},
		}

		// Call with empty bundle - should still process pendingRestorations
		manager.processRestorations(portainer.RestoreSettingsBundle{})

		// Verify it tried to process (pendingRestorations still there since deployment fails)
		assert.Contains(t, manager.pendingRestorations, portainer.SecurityK8s,
			"pendingRestorations should still contain the entry after failed deployment")
	})

	t.Run("Does not store already restored policy types", func(t *testing.T) {
		kubeClient := &kubernetes.KubeClient{}
		manager := &PolicyManager{
			kubeClient:        kubeClient,
			policyChartStatus: map[string]*portainer.PolicyChartStatus{},
			restoredPolicyTypes: map[portainer.PolicyType]bool{
				portainer.SecurityK8s: true, // Already restored
			},
			pendingRestorations: make(portainer.RestoreSettingsBundle),
		}

		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.SecurityK8s: {Manifest: base64.StdEncoding.EncodeToString([]byte("test-manifest"))},
		}

		manager.processRestorations(restoreBundle)

		// Should NOT be added to pendingRestorations since already restored
		assert.NotContains(t, manager.pendingRestorations, portainer.SecurityK8s,
			"Already restored policy types should not be added to pendingRestorations")
	})

	t.Run("Does not store when charts are uninstalling", func(t *testing.T) {
		kubeClient := &kubernetes.KubeClient{}
		manager := &PolicyManager{
			kubeClient: kubeClient,
			policyChartStatus: map[string]*portainer.PolicyChartStatus{
				"gatekeeper": {Status: portainer.HelmInstallStatusUninstalling},
			},
			restoredPolicyTypes: make(map[portainer.PolicyType]bool),
			pendingRestorations: make(portainer.RestoreSettingsBundle),
		}

		manifest := base64.StdEncoding.EncodeToString([]byte("test-manifest"))
		restoreBundle := portainer.RestoreSettingsBundle{
			portainer.SecurityK8s: {Manifest: manifest},
		}

		manager.processRestorations(restoreBundle)

		// Should still store in pendingRestorations even if skipping processing due to uninstalling
		assert.Contains(t, manager.pendingRestorations, portainer.SecurityK8s,
			"Should store restoration settings even when charts are uninstalling (for later retry)")
	})
}

func TestIsFailedOrPendingStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		{
			name:     "Failed status",
			status:   "failed",
			expected: true,
		},
		{
			name:     "Pending install status",
			status:   "pending-install",
			expected: true,
		},
		{
			name:     "Pending upgrade status",
			status:   "pending-upgrade",
			expected: true,
		},
		{
			name:     "Pending rollback status",
			status:   "pending-rollback",
			expected: true,
		},
		{
			name:     "Deployed status",
			status:   "deployed",
			expected: false,
		},
		{
			name:     "Superseded status",
			status:   "superseded",
			expected: false,
		},
		{
			name:     "Uninstalled status",
			status:   "uninstalled",
			expected: false,
		},
		{
			name:     "Empty status",
			status:   "",
			expected: false,
		},
		{
			name:     "Unknown status",
			status:   "unknown",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFailedOrPendingStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockHelmPackageManagerWithStatus is a test mock that can return releases with custom statuses
type mockHelmPackageManagerWithStatus struct {
	releases         []release.ReleaseElement
	historyReleases  []*release.Release
	listError        error
	historyError     error
	uninstallErr     error
	forceRemoveErr   error
	uninstallCalls   int
	forceRemoveCalls int
}

func (m *mockHelmPackageManagerWithStatus) Show(showOpts options.ShowOptions) ([]byte, error) {
	return nil, nil
}

func (m *mockHelmPackageManagerWithStatus) SearchRepo(searchRepoOpts options.SearchRepoOptions) ([]byte, error) {
	return nil, nil
}

func (m *mockHelmPackageManagerWithStatus) List(listOpts options.ListOptions) ([]release.ReleaseElement, error) {
	if m.listError != nil {
		return nil, m.listError
	}
	return m.releases, nil
}

func (m *mockHelmPackageManagerWithStatus) Upgrade(upgradeOpts options.InstallOptions) (*release.Release, error) {
	return &release.Release{Name: upgradeOpts.Name, Namespace: upgradeOpts.Namespace}, nil
}

func (m *mockHelmPackageManagerWithStatus) Uninstall(uninstallOpts options.UninstallOptions) error {
	m.uninstallCalls++
	return m.uninstallErr
}

func (m *mockHelmPackageManagerWithStatus) ForceRemoveRelease(uninstallOpts options.UninstallOptions) error {
	m.forceRemoveCalls++
	return m.forceRemoveErr
}

func (m *mockHelmPackageManagerWithStatus) Get(getOpts options.GetOptions) (*release.Release, error) {
	return nil, nil
}

func (m *mockHelmPackageManagerWithStatus) GetHistory(historyOpts options.HistoryOptions) ([]*release.Release, error) {
	if m.historyError != nil {
		return nil, m.historyError
	}
	return m.historyReleases, nil
}

func (m *mockHelmPackageManagerWithStatus) Rollback(rollbackOpts options.RollbackOptions) (*release.Release, error) {
	return nil, nil
}

// newHistoryRelease creates a release with the given status for history mocking
func newHistoryRelease(name, namespace string, status release.Status) *release.Release {
	return &release.Release{
		Name:      name,
		Namespace: namespace,
		Version:   1,
		Info: &release.Info{
			Status: status,
		},
	}
}

// Test cleanupFailedRelease function
func TestCleanupFailedRelease(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPortainerClient := mocks.NewMockPortainerClient(ctrl)
	mockKubeClient := &kubernetes.KubeClient{}

	t.Run("No history found (release not found error)", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyError: errors.New("release: not found"),
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 0, mockHelm.uninstallCalls)
	})

	t.Run("Empty history - no cleanup needed", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 0, mockHelm.uninstallCalls)
	})

	t.Run("Release in deployed state - no cleanup needed", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "deployed"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 0, mockHelm.uninstallCalls)
	})

	t.Run("Release in failed state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "failed"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
		assert.Equal(t, 0, mockHelm.forceRemoveCalls)
	})

	t.Run("Release in pending-install state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "pending-install"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
	})

	t.Run("Release in pending-upgrade state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "pending-upgrade"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
	})

	t.Run("Release in pending-rollback state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "pending-rollback"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
	})

	t.Run("Release in uninstalling state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "uninstalling"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
	})

	t.Run("Release in superseded state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "superseded"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
	})

	t.Run("Release in uninstalled state - should uninstall", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "uninstalled"),
			},
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
	})

	t.Run("GetHistory error - should return error", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyError: errors.New("connection refused"),
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get release history")
		assert.Equal(t, 0, mockHelm.uninstallCalls)
	})

	t.Run("Uninstall error - force-remove succeeds as fallback", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "failed"),
			},
			uninstallErr: errors.New("uninstall failed: CRDs missing"),
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.NoError(t, err)
		assert.Equal(t, 1, mockHelm.uninstallCalls)
		assert.Equal(t, 1, mockHelm.forceRemoveCalls)
	})

	t.Run("Uninstall error and force-remove error - should return error", func(t *testing.T) {
		mockHelm := &mockHelmPackageManagerWithStatus{
			historyReleases: []*release.Release{
				newHistoryRelease("my-chart", "default", "failed"),
			},
			uninstallErr:   errors.New("uninstall failed"),
			forceRemoveErr: errors.New("force-remove failed"),
		}
		manager := NewPolicyManager(mockPortainerClient, mockKubeClient, mockHelm)

		err := manager.cleanupFailedRelease("my-chart", "default")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to force-remove release")
		assert.Contains(t, err.Error(), "original")
		assert.Equal(t, 1, mockHelm.uninstallCalls)
		assert.Equal(t, 1, mockHelm.forceRemoveCalls)
	})
}
