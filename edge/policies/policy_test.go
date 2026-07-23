package policies

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/policies/helm"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/portainer/portainer/pkg/libhelm/options"
	"github.com/portainer/portainer/pkg/libhelm/release"
	"github.com/portainer/portainer/pkg/libhelm/sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	fips.InitFIPS(false)
}

// TestRestoreTypeForChart covers the chart-name → policy-type mapping.
func TestRestoreTypeForChart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		chart    string
		expected portainer.PolicyType
	}{
		{"portainer-registry-k8s", portainer.RegistryK8s},
		{"gatekeeper", portainer.SecurityK8s},
		{"portainer-security-k8s", portainer.SecurityK8s},
		{"unknown-chart", ""},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, helm.RestoreTypeForChart(tt.chart), tt.chart)
	}
}

// TestRestoreSettingsForPolicy covers the bundle-lookup helper.
func TestRestoreSettingsForPolicy(t *testing.T) {
	t.Parallel()

	registryManifest := "manifest-registry"
	bundle := portainer.RestoreSettingsBundle{
		portainer.RegistryK8s: {Manifest: registryManifest},
	}

	t.Run("matching chart returns settings", func(t *testing.T) {
		summaries := []portainer.PolicyChartSummary{{ChartName: "portainer-registry-k8s"}}
		rs := restoreSettingsForPolicy(summaries, bundle)
		if assert.NotNil(t, rs) {
			assert.Equal(t, registryManifest, rs.Manifest)
		}
	})

	t.Run("no matching chart returns nil", func(t *testing.T) {
		summaries := []portainer.PolicyChartSummary{{ChartName: "portainer-rbac-k8s"}}
		assert.Nil(t, restoreSettingsForPolicy(summaries, bundle))
	})

	t.Run("empty bundle returns nil", func(t *testing.T) {
		summaries := []portainer.PolicyChartSummary{{ChartName: "portainer-registry-k8s"}}
		assert.Nil(t, restoreSettingsForPolicy(summaries, portainer.RestoreSettingsBundle{}))
	})

	t.Run("empty manifest entry returns nil", func(t *testing.T) {
		b := portainer.RestoreSettingsBundle{portainer.RegistryK8s: {Manifest: ""}}
		summaries := []portainer.PolicyChartSummary{{ChartName: "portainer-registry-k8s"}}
		assert.Nil(t, restoreSettingsForPolicy(summaries, b))
	})
}

// --- Stubs for PolicyManager integration tests ---

type stubPMClient struct {
	client.PortainerClient
	getChartsFunc             func([]string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error)
	updateStatusesFunc        func([]portainer.PolicyChartStatus) error
	updatedStatuses           []portainer.PolicyChartStatus
	updatePolicyStatusesCalls int
}

func (c *stubPMClient) GetCharts(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
	if c.getChartsFunc != nil {
		return c.getChartsFunc(names)
	}
	return nil, nil, nil
}

func (c *stubPMClient) UpdatePolicyChartStatuses(statuses []portainer.PolicyChartStatus) error {
	c.updatePolicyStatusesCalls++
	c.updatedStatuses = statuses
	if c.updateStatusesFunc != nil {
		return c.updateStatusesFunc(statuses)
	}
	return nil
}

type stubPMHelmManager struct {
	sdk.HelmSDKPackageManager
	upgradeFunc   func(options.InstallOptions) (*release.Release, error)
	uninstallFunc func(options.UninstallOptions) error
}

func (m *stubPMHelmManager) Upgrade(opts options.InstallOptions) (*release.Release, error) {
	if m.upgradeFunc != nil {
		return m.upgradeFunc(opts)
	}
	return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
}

func (m *stubPMHelmManager) Uninstall(opts options.UninstallOptions) error {
	if m.uninstallFunc != nil {
		return m.uninstallFunc(opts)
	}
	return nil
}

func (m *stubPMHelmManager) GetHistory(opts options.HistoryOptions) ([]*release.Release, error) {
	return nil, errors.New("not found")
}

func newTestPolicyManager(pc *stubPMClient, hm *stubPMHelmManager) *PolicyManager {
	reporter := helm.NewChartStatusReporter()
	return NewPolicyManager(pc, nil, hm, nil, reporter, nil, 1)
}

func TestProcessPolicyHelmCharts_GetChartsFailure_MarksChartsFailed(t *testing.T) {
	t.Parallel()

	pc := &stubPMClient{
		getChartsFunc: func(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
			return nil, nil, errors.New("server error")
		},
	}
	hm := &stubPMHelmManager{}
	pm := newTestPolicyManager(pc, hm)

	summaries := []portainer.PolicyChartSummary{
		{PolicyID: 1, ChartName: "gatekeeper", Fingerprint: "fp1"},
		{PolicyID: 1, ChartName: "portainer-security-k8s", Fingerprint: "fp2"},
	}

	pm.ProcessPolicyHelmCharts(summaries)

	// All charts should be marked failed in statuses.
	require.NotNil(t, pc.updatedStatuses)
	for _, s := range pc.updatedStatuses {
		assert.Equal(t, portainer.HelmInstallStatusFailed, s.Status)
		assert.Contains(t, s.Message, "Failed to retrieve charts from server")
	}
}

func TestProcessPolicyHelmCharts_ZeroPolicyID_Skipped(t *testing.T) {
	t.Parallel()

	pc := &stubPMClient{}
	hm := &stubPMHelmManager{}
	pm := newTestPolicyManager(pc, hm)

	summaries := []portainer.PolicyChartSummary{
		{PolicyID: 0, ChartName: "gatekeeper", Fingerprint: "fp1"},
	}

	pm.ProcessPolicyHelmCharts(summaries)

	// No handlers should be created, no status updates.
	assert.Empty(t, pm.handlers)
}

func TestProcessPolicyHelmCharts_RemovesHandlerForMissingPolicy(t *testing.T) {
	t.Parallel()

	pc := &stubPMClient{}
	hm := &stubPMHelmManager{}
	pm := newTestPolicyManager(pc, hm)

	// First call: install policy 1's chart.
	summaries := []portainer.PolicyChartSummary{
		{PolicyID: 1, ChartName: "gatekeeper", Fingerprint: "fp1"},
	}
	pc.getChartsFunc = func(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
		return []portainer.PolicyChartBundle{{
			PolicyChartSummary: portainer.PolicyChartSummary{PolicyID: 1, ChartName: "gatekeeper", Fingerprint: "fp1"},
			Namespace:          "portainer",
			EncodedTgz:         base64.StdEncoding.EncodeToString([]byte("chart")),
			EncodedValues:      base64.StdEncoding.EncodeToString([]byte("values")),
		}}, nil, nil
	}
	pm.ProcessPolicyHelmCharts(summaries)
	require.Contains(t, pm.handlers, portainer.PolicyID(1))

	// Second call: empty summaries → policy 1 should be removed.
	pm.ProcessPolicyHelmCharts(nil)

	assert.Empty(t, pm.handlers, "handler should be removed when policy no longer in summaries")
}

func TestProcessPolicyHelmCharts_MultiPolicy_IndependentHandlers(t *testing.T) {
	t.Parallel()

	pc := &stubPMClient{
		getChartsFunc: func(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
			var bundles []portainer.PolicyChartBundle
			for _, name := range names {
				bundles = append(bundles, portainer.PolicyChartBundle{
					PolicyChartSummary: portainer.PolicyChartSummary{ChartName: name, Fingerprint: "fp-" + name},
					Namespace:          "portainer",
					EncodedTgz:         base64.StdEncoding.EncodeToString([]byte(name)),
					EncodedValues:      base64.StdEncoding.EncodeToString([]byte("val")),
				})
			}
			return bundles, nil, nil
		},
	}
	hm := &stubPMHelmManager{}
	pm := newTestPolicyManager(pc, hm)

	summaries := []portainer.PolicyChartSummary{
		{PolicyID: 1, ChartName: "gatekeeper", Fingerprint: "fp-gatekeeper"},
		{PolicyID: 2, ChartName: "portainer-registry-k8s", Fingerprint: "fp-portainer-registry-k8s"},
	}

	pm.ProcessPolicyHelmCharts(summaries)

	assert.Len(t, pm.handlers, 2, "each policy should get its own handler")
	assert.Contains(t, pm.handlers, portainer.PolicyID(1))
	assert.Contains(t, pm.handlers, portainer.PolicyID(2))
}
