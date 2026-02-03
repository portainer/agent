package policies

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/portainer/agent/internals/mocks"
	"github.com/portainer/agent/kubernetes"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
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

		// Even with no charts to install, GetCharts is called to get restore bundle
		mockPortainerClient.EXPECT().GetCharts([]string{}).Return([]portainer.PolicyChartBundle{}, portainer.RestoreSettingsBundle{}, nil)

		manager.ProcessPolicyHelmCharts(chartSummaries)

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
			assert.Equal(t, summary.Fingerprint, statuses[0].Fingerprint)
			assert.Equal(t, portainer.HelmInstallStatusFailed, statuses[0].Status)
			assert.Equal(t, "Failed to retrieve charts from server", statuses[0].Message)
			assert.NotZero(t, statuses[0].LastAttemptTime)

			return nil
		})

		manager.ProcessPolicyHelmCharts(chartSummaries)

		assert.Len(t, manager.policyChartStatus, 1)
		assert.Equal(t, portainer.HelmInstallStatusFailed, manager.policyChartStatus["chart1"].Status)
		assert.Equal(t, "Failed to retrieve charts from server", manager.policyChartStatus["chart1"].Message)

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
