package helm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/policyreconcile"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/libhelm/options"
	"github.com/portainer/portainer/pkg/libhelm/release"
	"github.com/portainer/portainer/pkg/libhelm/sdk"
	"github.com/portainer/portainer/pkg/libpolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubHelmPackageManager struct {
	sdk.HelmSDKPackageManager
	upgradeCalls   int
	uninstallCalls int
	upgradeFunc    func(options.InstallOptions) (*release.Release, error)
	uninstallFunc  func(options.UninstallOptions) error
	historyFunc    func(options.HistoryOptions) ([]*release.Release, error)
}

func (m *stubHelmPackageManager) Upgrade(opts options.InstallOptions) (*release.Release, error) {
	m.upgradeCalls++
	if m.upgradeFunc != nil {
		return m.upgradeFunc(opts)
	}
	return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
}

func (m *stubHelmPackageManager) GetHistory(opts options.HistoryOptions) ([]*release.Release, error) {
	if m.historyFunc != nil {
		return m.historyFunc(opts)
	}
	return nil, errors.New("not found")
}

func (m *stubHelmPackageManager) Uninstall(opts options.UninstallOptions) error {
	m.uninstallCalls++
	if m.uninstallFunc != nil {
		return m.uninstallFunc(opts)
	}
	return nil
}

func TestHelmHandlerApply_UsesBundledChartAndReportsStatus(t *testing.T) {
	// DEV_KUBECONFIG_PATH forces exec.InClusterKubeAccess() to return a non-nil
	// sentinel regardless of whether the test runs inside a cluster, so the
	// assertion below actually fails if installChartBundle stops wiring
	// KubernetesClusterAccess, rather than trivially comparing nil to nil.
	t.Setenv("DEV_KUBECONFIG_PATH", "test-kubeconfig")

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			assert.Equal(t, "gatekeeper", opts.Name)
			assert.Equal(t, "portainer", opts.Namespace)
			assert.True(t, opts.Wait)
			assert.True(t, opts.TakeOwnership)
			assert.True(t, opts.CreateNamespace)
			assert.True(t, opts.Atomic)
			assert.Equal(t, maxChartHistory, opts.MaxHistory)
			assert.Equal(t, &options.KubernetesClusterAccess{}, opts.KubernetesClusterAccess)
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	reporter := NewChartStatusReporter()
	handler := NewHandler(nil, manager, nil, nil, reporter, nil)(7).(*HelmHandler)

	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 7, ChartName: "gatekeeper", Fingerprint: "fp1"},
		},
		Bundles: []portainer.PolicyChartBundle{
			{
				PolicyChartSummary: portainer.PolicyChartSummary{
					PolicyID:    7,
					ChartName:   "gatekeeper",
					Fingerprint: "fp1",
				},
				Namespace:     "portainer",
				EncodedTgz:    base64.StdEncoding.EncodeToString([]byte("chart")),
				EncodedValues: base64.StdEncoding.EncodeToString([]byte("values")),
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, handler.Apply(context.Background(), raw))
	assert.Equal(t, 1, manager.upgradeCalls)

	status := handler.Status()
	assert.Equal(t, portainer.PolicyID(7), status.PolicyID)
	assert.Equal(t, helmPolicyType, status.Type)
	assert.Equal(t, policyreconcile.StatusApplied, status.Status)
	assert.Equal(t, libpolicy.HelmPolicyFingerprint([]portainer.PolicyChartSummary{
		{PolicyID: 7, ChartName: "gatekeeper", Fingerprint: "fp1"},
	}), status.Fingerprint)

	statuses := handler.ChartStatuses(42)
	require.Len(t, statuses, 1)
	assert.Equal(t, portainer.EndpointID(42), statuses[0].EnvironmentID)
	assert.Equal(t, "gatekeeper", statuses[0].ChartName)
	assert.Equal(t, "fp1", statuses[0].Fingerprint)
	assert.Equal(t, portainer.HelmInstallStatusInstalled, statuses[0].Status)

	reported := reporter.Snapshot()
	require.Len(t, reported, 1)
	assert.Equal(t, "gatekeeper", reported[0].ChartName)
}

func TestHelmHandlerMarkChartsFailed_RecordsRetryableFailure(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, &stubHelmPackageManager{}, nil, nil, nil, nil)(9).(*HelmHandler)

	handler.MarkChartsFailed(
		[]portainer.PolicyChartSummary{{PolicyID: 9, ChartName: "portainer-rbac-k8s", Fingerprint: "fp1"}},
		"Failed to retrieve charts from server",
		errors.New("server unavailable"),
	)

	statuses := handler.ChartStatuses(11)
	require.Len(t, statuses, 1)
	assert.Equal(t, portainer.EndpointID(11), statuses[0].EnvironmentID)
	assert.Equal(t, "portainer-rbac-k8s", statuses[0].ChartName)
	// MarkChartsFailed creates new chart records (no prior fingerprint), so fingerprint is empty.
	assert.Empty(t, statuses[0].Fingerprint, "new chart records have no prior fingerprint")
	assert.Equal(t, portainer.HelmInstallStatusFailed, statuses[0].Status)
	assert.Contains(t, statuses[0].Message, "Failed to retrieve charts from server")
	assert.Contains(t, statuses[0].Message, "server unavailable")

	status := handler.Status()
	assert.Equal(t, portainer.PolicyID(9), status.PolicyID)
	assert.Equal(t, helmPolicyType, status.Type)
	assert.Equal(t, policyreconcile.StatusFailed, status.Status)
	assert.Contains(t, status.Message, "server unavailable")
}

func TestHelmHandlerRemove_MarksChartsUninstalling(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{}
	handler := NewHandler(nil, manager, nil, nil, NewChartStatusReporter(), nil)(9).(*HelmHandler)
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{
		ChartName:   "portainer-rbac-k8s",
		Fingerprint: "fp1",
		Namespace:   "portainer",
		Status:      portainer.HelmInstallStatusInstalled,
	}

	require.NoError(t, handler.Remove(context.Background()))

	assert.Equal(t, 1, manager.uninstallCalls)
	statuses := handler.ChartStatuses(11)
	require.Len(t, statuses, 1)
	assert.Equal(t, portainer.HelmInstallStatusUninstalling, statuses[0].Status)
	assert.Equal(t, "Uninstalling chart", statuses[0].Message)
}

// stubPortainerClient implements client.PortainerClient for tests that need GetCharts.
type stubPortainerClient struct {
	client.PortainerClient
	getChartsFunc func([]string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error)
}

func (c *stubPortainerClient) GetCharts(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
	if c.getChartsFunc != nil {
		return c.getChartsFunc(names)
	}
	return nil, nil, errors.New("GetCharts not configured")
}

func TestRegistration_NilNsDriftCoordinator_OmittedFromPollHooks(t *testing.T) {
	t.Parallel()

	coordinator := NewRestoreCoordinator(nil)
	reg := Registration(nil, nil, nil, coordinator, nil, nil)

	require.Len(t, reg.PollHooks, 1, "a nil nsDriftCoordinator must not be added as a PollHook")
	assert.Same(t, coordinator, reg.PollHooks[0])

	// A nil PollHook entry panics when PollService.reconcilePolicies calls
	// Tick without a nil check (C9S-325); guard against a regression here.
	assert.NotPanics(t, func() {
		for _, hook := range reg.PollHooks {
			hook.Tick(context.Background(), nil)
		}
	})
}

func TestReconcileCharts_MultiChart_SecurityK8sScenario(t *testing.T) {
	t.Parallel()

	var installedCharts []string
	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			installedCharts = append(installedCharts, opts.Name)
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	reporter := NewChartStatusReporter()
	handler := NewHandler(nil, manager, nil, nil, reporter, nil)(10).(*HelmHandler)

	charts := []portainer.PolicyChartSummary{
		{PolicyID: 10, ChartName: "gatekeeper", Fingerprint: "fp-gk"},
		{PolicyID: 10, ChartName: "portainer-security-k8s", Fingerprint: "fp-sec"},
	}
	bundles := []portainer.PolicyChartBundle{
		{
			PolicyChartSummary: charts[0],
			Namespace:          "portainer",
			EncodedTgz:         base64.StdEncoding.EncodeToString([]byte("gk-chart")),
			EncodedValues:      base64.StdEncoding.EncodeToString([]byte("gk-values")),
		},
		{
			PolicyChartSummary: charts[1],
			Namespace:          "portainer",
			EncodedTgz:         base64.StdEncoding.EncodeToString([]byte("sec-chart")),
			EncodedValues:      base64.StdEncoding.EncodeToString([]byte("sec-values")),
		},
	}

	raw, err := json.Marshal(HelmPolicyConfig{Charts: charts, Bundles: bundles})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 2, manager.upgradeCalls, "both charts should be installed")
	assert.Contains(t, installedCharts, "gatekeeper")
	assert.Contains(t, installedCharts, "portainer-security-k8s")

	statuses := handler.ChartStatuses(1)
	assert.Len(t, statuses, 2, "both charts should have status entries")
	for _, s := range statuses {
		assert.Equal(t, portainer.HelmInstallStatusInstalled, s.Status)
	}
}

func TestReconcileCharts_FingerprintUnchanged_NoInstall(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{}
	handler := NewHandler(nil, manager, nil, nil, nil, nil)(5).(*HelmHandler)

	// Pre-populate as already installed at fp1.
	handler.installedCharts["gatekeeper"] = chartRecord{
		ChartName:   "gatekeeper",
		Fingerprint: "fp1",
		Namespace:   "portainer",
		Status:      portainer.HelmInstallStatusInstalled,
	}

	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 0, manager.upgradeCalls, "no install when fingerprint unchanged")
}

func TestReconcileCharts_ChartRemovedFromPolicy_Uninstalled(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{}
	handler := NewHandler(nil, manager, nil, nil, nil, nil)(5).(*HelmHandler)

	// Pre-populate two charts as installed.
	handler.installedCharts["gatekeeper"] = chartRecord{
		ChartName: "gatekeeper", Fingerprint: "fp1", Namespace: "portainer",
		Status: portainer.HelmInstallStatusInstalled,
	}
	handler.installedCharts["portainer-security-k8s"] = chartRecord{
		ChartName: "portainer-security-k8s", Fingerprint: "fp2", Namespace: "portainer",
		Status: portainer.HelmInstallStatusInstalled,
	}

	// Apply with only gatekeeper — portainer-security-k8s should be uninstalled.
	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 1, manager.uninstallCalls, "removed chart should be uninstalled")
	assert.Contains(t, handler.installedCharts, "gatekeeper")
	assert.NotContains(t, handler.installedCharts, "portainer-security-k8s")
}

func TestReconcileCharts_OnDemandFetch_WhenNoBundlesSupplied(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	pc := &stubPortainerClient{
		getChartsFunc: func(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
			assert.Equal(t, []string{"gatekeeper"}, names, "only changed chart should be fetched")
			return []portainer.PolicyChartBundle{{
				PolicyChartSummary: portainer.PolicyChartSummary{
					PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1",
				},
				Namespace:     "portainer",
				EncodedTgz:    base64.StdEncoding.EncodeToString([]byte("chart-data")),
				EncodedValues: base64.StdEncoding.EncodeToString([]byte("values")),
			}}, nil, nil
		},
	}
	handler := NewHandler(nil, manager, pc, nil, nil, nil)(5).(*HelmHandler)

	// Apply with no bundles — should trigger on-demand fetch via GetCharts.
	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 1, manager.upgradeCalls)
	assert.Equal(t, portainer.HelmInstallStatusInstalled, handler.installedCharts["gatekeeper"].Status)
}

func TestReconcileCharts_FetchFailure_AllChartsFailed(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{}
	pc := &stubPortainerClient{
		getChartsFunc: func(names []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
			return nil, nil, errors.New("server unavailable")
		},
	}
	handler := NewHandler(nil, manager, pc, nil, nil, nil)(5).(*HelmHandler)

	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"},
			{PolicyID: 5, ChartName: "portainer-security-k8s", Fingerprint: "fp2"},
		},
	})
	require.NoError(t, err)
	err = handler.Apply(context.Background(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server unavailable")

	assert.Equal(t, 0, manager.upgradeCalls, "no installs on fetch failure")
	for _, rec := range handler.installedCharts {
		assert.Equal(t, portainer.HelmInstallStatusFailed, rec.Status)
		assert.Contains(t, rec.Message, "Failed to retrieve charts from server")
		// Charts created during reconcile have no prior fingerprint, so still empty.
		assert.Empty(t, rec.Fingerprint, "new chart records have no prior fingerprint")
	}

	// Policy-level status should have the fingerprint set for server persistence.
	status := handler.Status()
	assert.Equal(t, policyreconcile.StatusFailed, status.Status)
	expectedFP := libpolicy.HelmPolicyFingerprint([]portainer.PolicyChartSummary{
		{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"},
		{PolicyID: 5, ChartName: "portainer-security-k8s", Fingerprint: "fp2"},
	})
	assert.Equal(t, expectedFP, status.Fingerprint,
		"policy fingerprint should be set on failure so server can persist the status")
}

func TestReconcileCharts_PartialFailure_OneChartFails(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			if opts.Name == "gatekeeper" {
				return nil, errors.New("helm install failed")
			}
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	reporter := NewChartStatusReporter()
	handler := NewHandler(nil, manager, nil, nil, reporter, nil)(10).(*HelmHandler)

	charts := []portainer.PolicyChartSummary{
		{PolicyID: 10, ChartName: "gatekeeper", Fingerprint: "fp1"},
		{PolicyID: 10, ChartName: "portainer-security-k8s", Fingerprint: "fp2"},
	}
	bundles := []portainer.PolicyChartBundle{
		{
			PolicyChartSummary: charts[0],
			Namespace:          "portainer",
			EncodedTgz:         base64.StdEncoding.EncodeToString([]byte("gk")),
			EncodedValues:      base64.StdEncoding.EncodeToString([]byte("v")),
		},
		{
			PolicyChartSummary: charts[1],
			Namespace:          "portainer",
			EncodedTgz:         base64.StdEncoding.EncodeToString([]byte("sec")),
			EncodedValues:      base64.StdEncoding.EncodeToString([]byte("v")),
		},
	}

	raw, err := json.Marshal(HelmPolicyConfig{Charts: charts, Bundles: bundles})
	require.NoError(t, err)
	err = handler.Apply(context.Background(), raw)
	require.Error(t, err, "partial failure should return error")
	assert.Contains(t, err.Error(), "gatekeeper", "error should identify the failing chart")
	assert.Contains(t, err.Error(), "helm install failed", "error should contain the actual helm error")

	assert.Equal(t, 2, manager.upgradeCalls, "both charts attempted")

	gk := handler.installedCharts["gatekeeper"]
	assert.Equal(t, portainer.HelmInstallStatusFailed, gk.Status)
	assert.Contains(t, gk.Message, "helm install failed", "chart record should contain actual error")

	sec := handler.installedCharts["portainer-security-k8s"]
	assert.Equal(t, portainer.HelmInstallStatusInstalled, sec.Status)

	// Policy-level status should preserve fingerprint and surface the chart error.
	status := handler.Status()
	assert.Equal(t, policyreconcile.StatusFailed, status.Status)
	assert.Contains(t, status.Message, "gatekeeper")
	assert.Contains(t, status.Message, "helm install failed")
	expectedFP := libpolicy.HelmPolicyFingerprint(charts)
	assert.Equal(t, expectedFP, status.Fingerprint,
		"policy fingerprint should be set on failure so server can persist the status")
}

func TestReconcileCharts_FingerprintChanged_Reinstall(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	handler := NewHandler(nil, manager, nil, nil, nil, nil)(5).(*HelmHandler)

	// Pre-populate as installed with old fingerprint.
	handler.installedCharts["gatekeeper"] = chartRecord{
		ChartName:   "gatekeeper",
		Fingerprint: "fp-old",
		Namespace:   "portainer",
		Status:      portainer.HelmInstallStatusInstalled,
	}

	// Apply with new fingerprint and bundle.
	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp-new"},
		},
		Bundles: []portainer.PolicyChartBundle{{
			PolicyChartSummary: portainer.PolicyChartSummary{
				PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp-new",
			},
			Namespace:     "portainer",
			EncodedTgz:    base64.StdEncoding.EncodeToString([]byte("chart")),
			EncodedValues: base64.StdEncoding.EncodeToString([]byte("val")),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 1, manager.upgradeCalls, "fingerprint changed → reinstall")
	assert.Equal(t, "fp-new", handler.installedCharts["gatekeeper"].Fingerprint)
}

func (m *stubHelmPackageManager) ForceRemoveRelease(opts options.UninstallOptions) error {
	return nil
}

// deployedRelease builds a release-history entry with the given status and chart path.
// An empty chartPath signals an externally-managed release (no portainer annotation).
func deployedRelease(name, namespace, status, chartPath string) *release.Release {
	return &release.Release{
		Name:           name,
		Namespace:      namespace,
		Info:           &release.Info{Status: release.Status(status)},
		ChartReference: release.ChartReference{ChartPath: chartPath},
	}
}

func newHelmBundle(policyID portainer.PolicyID, chartName, releaseName, fingerprint string) portainer.PolicyChartBundle {
	return portainer.PolicyChartBundle{
		PolicyChartSummary: portainer.PolicyChartSummary{PolicyID: policyID, ChartName: chartName, Fingerprint: fingerprint},
		ReleaseName:        releaseName,
		Namespace:          "portainer",
		EncodedTgz:         base64.StdEncoding.EncodeToString([]byte("chart")),
		EncodedValues:      base64.StdEncoding.EncodeToString([]byte("values")),
	}
}

func TestReconcileCharts_ExternallyManagedRelease_ReportsConflict(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		historyFunc: func(opts options.HistoryOptions) ([]*release.Release, error) {
			// Deployed release with no portainer/chart-path annotation → externally managed.
			return []*release.Release{deployedRelease(opts.Name, opts.Namespace, "deployed", "")}, nil
		},
	}
	reporter := NewChartStatusReporter()
	handler := NewHandler(nil, manager, nil, nil, reporter, nil)(5).(*HelmHandler)

	charts := []portainer.PolicyChartSummary{{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"}}
	raw, err := json.Marshal(HelmPolicyConfig{
		Charts:  charts,
		Bundles: []portainer.PolicyChartBundle{newHelmBundle(5, "gatekeeper", "", "fp1")},
	})
	require.NoError(t, err)

	err = handler.Apply(context.Background(), raw)
	require.Error(t, err, "conflict should surface as a policy-level failure")

	assert.Equal(t, 0, manager.upgradeCalls, "no upgrade when the release is externally managed")
	assert.Equal(t, 0, manager.uninstallCalls, "externally-managed release must not be uninstalled")

	rec := handler.installedCharts["gatekeeper"]
	assert.Equal(t, portainer.HelmInstallStatusConflict, rec.Status)
	assert.Equal(t, externalReleaseConflictMessage, rec.Message)

	status := handler.Status()
	assert.Equal(t, policyreconcile.StatusFailed, status.Status, "policy status should fail on conflict")
}

func TestReconcileCharts_PortainerManagedNonDeployed_CleanedUpThenInstalled(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		historyFunc: func(opts options.HistoryOptions) ([]*release.Release, error) {
			// Portainer-managed (chart path set) but failed → cleanup, then install.
			return []*release.Release{deployedRelease(opts.Name, opts.Namespace, "failed", "/charts/gatekeeper.tgz")}, nil
		},
	}
	handler := NewHandler(nil, manager, nil, nil, NewChartStatusReporter(), nil)(5).(*HelmHandler)

	raw, err := json.Marshal(HelmPolicyConfig{
		Charts:  []portainer.PolicyChartSummary{{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"}},
		Bundles: []portainer.PolicyChartBundle{newHelmBundle(5, "gatekeeper", "", "fp1")},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 1, manager.uninstallCalls, "non-deployed Portainer-managed release should be cleaned up")
	assert.Equal(t, 1, manager.upgradeCalls, "install proceeds after cleanup")
	assert.Equal(t, portainer.HelmInstallStatusInstalled, handler.installedCharts["gatekeeper"].Status)
}

func TestReconcileCharts_ReleaseNameHonored(t *testing.T) {
	t.Parallel()

	var upgradeName, uninstallName string
	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			upgradeName = opts.Name
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
		uninstallFunc: func(opts options.UninstallOptions) error {
			uninstallName = opts.Name
			return nil
		},
	}
	handler := NewHandler(nil, manager, nil, nil, NewChartStatusReporter(), nil)(5).(*HelmHandler)

	// Install with a ReleaseName that differs from the chart name.
	raw, err := json.Marshal(HelmPolicyConfig{
		Charts:  []portainer.PolicyChartSummary{{PolicyID: 5, ChartName: "portainer-observability-k8s", Fingerprint: "fp1"}},
		Bundles: []portainer.PolicyChartBundle{newHelmBundle(5, "portainer-observability-k8s", "kubernetes-agent", "fp1")},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))
	assert.Equal(t, "kubernetes-agent", upgradeName, "Upgrade must use ReleaseName, not ChartName")
	assert.Equal(t, "kubernetes-agent", handler.installedCharts["portainer-observability-k8s"].ReleaseName)

	// Remove the policy — uninstall must target the release name too.
	require.NoError(t, handler.Remove(context.Background()))
	assert.Equal(t, "kubernetes-agent", uninstallName, "Uninstall must use the tracked ReleaseName")
}

func TestReconcileCharts_FailedChart_RetriedOnSameFingerprint(t *testing.T) {
	t.Parallel()

	callCount := 0
	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			callCount++
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	handler := NewHandler(nil, manager, nil, nil, nil, nil)(5).(*HelmHandler)

	// Pre-populate as failed with fingerprint preserved (retry scenario).
	// Retry is driven by status != Installed, not by fingerprint mismatch.
	handler.installedCharts["gatekeeper"] = chartRecord{
		ChartName:   "gatekeeper",
		Fingerprint: "fp1", // preserved on failure
		Namespace:   "portainer",
		Status:      portainer.HelmInstallStatusFailed,
	}

	raw, err := json.Marshal(HelmPolicyConfig{
		Charts: []portainer.PolicyChartSummary{
			{PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1"},
		},
		Bundles: []portainer.PolicyChartBundle{{
			PolicyChartSummary: portainer.PolicyChartSummary{
				PolicyID: 5, ChartName: "gatekeeper", Fingerprint: "fp1",
			},
			Namespace:     "portainer",
			EncodedTgz:    base64.StdEncoding.EncodeToString([]byte("chart")),
			EncodedValues: base64.StdEncoding.EncodeToString([]byte("val")),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, handler.Apply(context.Background(), raw))

	assert.Equal(t, 1, callCount, "failed chart should be retried even with same fingerprint")
	assert.Equal(t, portainer.HelmInstallStatusInstalled, handler.installedCharts["gatekeeper"].Status)
}
