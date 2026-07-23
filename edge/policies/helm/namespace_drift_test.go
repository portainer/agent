package helm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/libhelm/options"
	"github.com/portainer/portainer/pkg/libhelm/release"
	libhelmtypes "github.com/portainer/portainer/pkg/libhelm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubNamespaceLister returns a namespace snapshot on each call. listFunc, if
// set, is given the 1-based call number so tests can vary the snapshot across
// ticks; otherwise the same fixed snapshot is returned every time.
type stubNamespaceLister struct {
	calls    int
	listFunc func(call int) (map[string]string, error)
}

func (s *stubNamespaceLister) ListNamespaces(context.Context) (map[string]string, error) {
	s.calls++
	if s.listFunc != nil {
		return s.listFunc(s.calls)
	}
	return map[string]string{"default": "1"}, nil
}

func rbacBundleForTest() portainer.PolicyChartBundle {
	return portainer.PolicyChartBundle{
		PolicyChartSummary: portainer.PolicyChartSummary{
			ChartName:   "portainer-rbac-k8s",
			Fingerprint: "fp1",
		},
		Namespace:     "portainer",
		EncodedTgz:    base64.StdEncoding.EncodeToString([]byte("chart")),
		EncodedValues: base64.StdEncoding.EncodeToString([]byte("values")),
	}
}

// newDriftTestHandler builds a HelmHandler wired to coordinator (may be nil)
// with lister installed directly as its namespaceLister, since NewHandler only
// derives namespaceLister from a real (non-nil) *kubernetes.KubeClient.
func newDriftTestHandler(manager libhelmtypes.HelmPackageManager, coordinator *NamespaceDriftCoordinator, lister NamespaceLister) *HelmHandler {
	handler := NewHandler(nil, manager, nil, nil, nil, coordinator)(5).(*HelmHandler)
	handler.namespaceLister = lister
	return handler
}

func TestNamespaceDriftCoordinator_ReappliesInstalledDriftSensitiveChart(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	coordinator := NewNamespaceDriftCoordinator()
	handler := newDriftTestHandler(manager, coordinator, &stubNamespaceLister{})

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{
		ChartName: "portainer-rbac-k8s",
		Namespace: "portainer",
		Status:    portainer.HelmInstallStatusInstalled,
	}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	// First tick has no prior namespace snapshot to compare against, so it
	// reapplies to establish the baseline.
	coordinator.Tick(context.Background(), nil)
	assert.Equal(t, 1, manager.upgradeCalls, "the first tick should reapply the installed drift-sensitive chart")
}

func TestNamespaceDriftCoordinator_SkipsChartsNotInstalledOrWithoutCachedBundle(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{}
	coordinator := NewNamespaceDriftCoordinator()
	handler := newDriftTestHandler(manager, coordinator, &stubNamespaceLister{})

	handler.mu.Lock()
	// Installed, but namespaceDriftSensitiveCharts only tracks "portainer-rbac-k8s".
	handler.installedCharts["gatekeeper"] = chartRecord{ChartName: "gatekeeper", Status: portainer.HelmInstallStatusInstalled}
	handler.lastBundles["gatekeeper"] = portainer.PolicyChartBundle{
		PolicyChartSummary: portainer.PolicyChartSummary{ChartName: "gatekeeper"},
	}
	// rbac chart tracked but never successfully installed, so it has no cached bundle.
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: portainer.HelmInstallStatusInstalling}
	handler.mu.Unlock()

	coordinator.Tick(context.Background(), nil)
	assert.Equal(t, 0, manager.upgradeCalls, "neither an unrelated chart nor a not-yet-installed chart should be reapplied")
}

func TestNamespaceDriftCoordinator_UnregisterStopsReapply(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	// A distinct snapshot per call so the second tick still detects a change
	// (otherwise it would skip for the wrong reason: no drift, not unregistration).
	lister := &stubNamespaceLister{
		listFunc: func(call int) (map[string]string, error) {
			return map[string]string{"default": fmt.Sprintf("gen-%d", call)}, nil
		},
	}
	coordinator := NewNamespaceDriftCoordinator()
	handler := newDriftTestHandler(manager, coordinator, lister)

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: portainer.HelmInstallStatusInstalled}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	coordinator.Tick(context.Background(), nil)
	require.Equal(t, 1, manager.upgradeCalls)

	coordinator.unregister(5)

	coordinator.Tick(context.Background(), nil)
	assert.Equal(t, 1, manager.upgradeCalls, "an unregistered handler must not be reapplied")
}

func TestNamespaceDriftCoordinator_SkipsReapplyWhenNamespacesUnchanged(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	// The stub lister returns the same fixed snapshot every time, so only the
	// baseline-establishing first tick should trigger a reapply.
	coordinator := NewNamespaceDriftCoordinator()
	handler := newDriftTestHandler(manager, coordinator, &stubNamespaceLister{})

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: portainer.HelmInstallStatusInstalled}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	for range 5 {
		coordinator.Tick(context.Background(), nil)
	}

	assert.Equal(t, 1, manager.upgradeCalls, "an unchanged namespace list must not trigger repeated reapplies")
}

func TestNamespaceDriftCoordinator_ReappliesWhenNamespacesChange(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	lister := &stubNamespaceLister{
		listFunc: func(call int) (map[string]string, error) {
			return map[string]string{"default": fmt.Sprintf("gen-%d", call)}, nil
		},
	}
	coordinator := NewNamespaceDriftCoordinator()
	handler := newDriftTestHandler(manager, coordinator, lister)

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: portainer.HelmInstallStatusInstalled}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	for i := 1; i <= 3; i++ {
		coordinator.Tick(context.Background(), nil)
		assert.Equal(t, i, manager.upgradeCalls, "each tick with a changed namespace snapshot should reapply")
	}
}

func TestNamespaceDriftCoordinator_ReportsChartFailureViaChartReporter(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return nil, errors.New("upgrade failed")
		},
	}
	coordinator := NewNamespaceDriftCoordinator()
	reporter := NewChartStatusReporter()
	handler := NewHandler(nil, manager, nil, nil, reporter, coordinator)(5).(*HelmHandler)
	handler.namespaceLister = &stubNamespaceLister{}

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: portainer.HelmInstallStatusInstalled}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	coordinator.Tick(context.Background(), nil)

	snapshot := reporter.Snapshot()
	require.Len(t, snapshot, 1)
	assert.Equal(t, "portainer-rbac-k8s", snapshot[0].ChartName)
	assert.Equal(t, portainer.HelmInstallStatusFailed, snapshot[0].Status)
}

func TestNamespaceDriftCoordinator_RetriesOnNextPollAfterFailedReapply(t *testing.T) {
	t.Parallel()

	fail := true
	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			if fail {
				return nil, errors.New("upgrade failed")
			}
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	// Fixed namespace snapshot: the retry must come from the failed reapply
	// itself, not from the namespace list changing again.
	coordinator := NewNamespaceDriftCoordinator()
	handler := newDriftTestHandler(manager, coordinator, &stubNamespaceLister{})

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: portainer.HelmInstallStatusInstalled}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	coordinator.Tick(context.Background(), nil)
	require.Equal(t, 1, manager.upgradeCalls, "first tick should attempt the reapply")

	coordinator.Tick(context.Background(), nil)
	assert.Equal(t, 2, manager.upgradeCalls, "a failed reapply must be retried on the next poll even with no further namespace change")

	fail = false
	coordinator.Tick(context.Background(), nil)
	assert.Equal(t, 3, manager.upgradeCalls, "retries continue until the reapply succeeds")

	coordinator.Tick(context.Background(), nil)
	assert.Equal(t, 3, manager.upgradeCalls, "once the reapply succeeds, the namespace snapshot advances and no further reapply happens without a new change")
}

func TestNamespaceDriftCoordinator_DoesNotResurrectRemovedPolicy(t *testing.T) {
	t.Parallel()

	manager := &stubHelmPackageManager{
		upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
			return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
		},
	}
	handler := newDriftTestHandler(manager, nil, &stubNamespaceLister{})

	handler.mu.Lock()
	handler.installedCharts["portainer-rbac-k8s"] = chartRecord{
		ChartName: "portainer-rbac-k8s",
		Namespace: "portainer",
		Status:    portainer.HelmInstallStatusInstalled,
	}
	handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
	handler.mu.Unlock()

	// Simulates the race a reapply can lose: a stale goroutine already captured
	// this handler and its cached bundle before the policy was removed. By the
	// time it actually calls reapplyNamespaceDriftCharts, Remove() has already
	// run to completion (marked the chart Uninstalling and uninstalled the
	// release) — the stale call must not resurrect it.
	require.NoError(t, handler.Remove(context.Background()))
	require.Equal(t, 1, manager.uninstallCalls, "sanity check: Remove should have uninstalled the release")

	handler.reapplyNamespaceDriftCharts(context.Background())
	assert.Equal(t, 0, manager.upgradeCalls, "a reapply arriving after Remove must not resurrect the uninstalled release")
}

func TestNamespaceDriftReapplyEligible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rec  chartRecord
		ok   bool
		want bool
	}{
		{name: "missing record", ok: false, want: false},
		{name: "installed", rec: chartRecord{Status: portainer.HelmInstallStatusInstalled}, ok: true, want: true},
		{name: "failed retried so a bad reapply self-heals", rec: chartRecord{Status: portainer.HelmInstallStatusFailed}, ok: true, want: true},
		{name: "uninstalling skipped, policy is being torn down", rec: chartRecord{Status: portainer.HelmInstallStatusUninstalling}, ok: true, want: false},
		{name: "conflict skipped, release is externally managed", rec: chartRecord{Status: portainer.HelmInstallStatusConflict}, ok: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, namespaceDriftReapplyEligible(tt.rec, tt.ok))
		})
	}
}

func TestReapplyNamespaceDriftCharts_SkipsUninstallingOrConflictCharts(t *testing.T) {
	t.Parallel()

	for _, status := range []portainer.HelmInstallStatus{portainer.HelmInstallStatusUninstalling, portainer.HelmInstallStatusConflict} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			manager := &stubHelmPackageManager{
				upgradeFunc: func(opts options.InstallOptions) (*release.Release, error) {
					return &release.Release{Name: opts.Name, Namespace: opts.Namespace}, nil
				},
			}
			handler := NewHandler(nil, manager, nil, nil, nil, nil)(5).(*HelmHandler)

			handler.mu.Lock()
			handler.installedCharts["portainer-rbac-k8s"] = chartRecord{ChartName: "portainer-rbac-k8s", Status: status}
			handler.lastBundles["portainer-rbac-k8s"] = rbacBundleForTest()
			handler.mu.Unlock()

			handler.reapplyNamespaceDriftCharts(context.Background())
			assert.Equal(t, 0, manager.upgradeCalls, "%s charts must not be reapplied", status)
		})
	}
}
