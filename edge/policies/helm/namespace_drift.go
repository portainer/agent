package helm

import (
	"context"
	"maps"
	"os"
	"sync"

	portainer "github.com/portainer/portainer/api"
	"github.com/rs/zerolog/log"

	"github.com/portainer/agent/policyreconcile"
)

// namespaceDriftSensitiveCharts lists chart names whose template renders
// against the live namespace list via Helm's `lookup` function (see
// all_namespace_role_bindings.yaml in the rbac-k8s chart). For these charts, a
// server-computed Fingerprint match does not guarantee the last-rendered
// manifest is still current: the server has no live cluster access when
// generating chart values (notably for edge-async environments), so nothing
// server-side ever changes that fingerprint when the cluster's namespaces do.
// This covers namespaces being created or deleted, and also a namespace being
// relabeled in place (e.g. gaining/losing io.portainer.kubernetes.namespace.system) -
// see checkNamespaceDrift for why a relabel is detected too, not just add/remove. C9S-325.
var namespaceDriftSensitiveCharts = map[string]bool{
	"portainer-rbac-k8s":             true,
	"portainer-network-security-k8s": true,
}

// NamespaceLister lists the current namespaces on a cluster, keyed by name
// with resourceVersion as the value. Implemented by *kubernetes.KubeClient.
type NamespaceLister interface {
	ListNamespaces(ctx context.Context) (map[string]string, error)
}

// compile-time assertion that NamespaceDriftCoordinator implements policyreconcile.PollHook.
var _ policyreconcile.PollHook = (*NamespaceDriftCoordinator)(nil)

// NamespaceDriftCoordinator is a thin per-poll registry: PollHook
// registration happens once per policy type at startup, before any
// HelmHandler exists, so something has to track which handlers are currently
// live and dispatch Tick to each of them. The actual drift-detection logic
// (namespace snapshot comparison, reapply) lives on HelmHandler itself — see
// checkNamespaceDrift — using that handler's own kubeClient. There is
// deliberately no separate interval here: Tick runs on every poll, so the
// poll loop's own cadence is the throttle.
type NamespaceDriftCoordinator struct {
	mu       sync.Mutex
	handlers map[portainer.PolicyID]*HelmHandler
}

// NewNamespaceDriftCoordinator returns an empty handler registry for
// namespace-drift checks; see NamespaceDriftCoordinator.
func NewNamespaceDriftCoordinator() *NamespaceDriftCoordinator {
	return &NamespaceDriftCoordinator{
		handlers: make(map[portainer.PolicyID]*HelmHandler),
	}
}

func (c *NamespaceDriftCoordinator) register(policyID portainer.PolicyID, h *HelmHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[policyID] = h
}

func (c *NamespaceDriftCoordinator) unregister(policyID portainer.PolicyID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.handlers, policyID)
}

// Tick implements policyreconcile.PollHook. It is called on every poll
// regardless of whether any policy's desired state changed, and simply
// dispatches to each live handler's own checkNamespaceDrift. Namespace-drift
// reapply never contributes ActualState of its own — installChartBundle
// already records per-chart status, reported via the normal chartReporter
// path, so Tick always returns nil here.
func (c *NamespaceDriftCoordinator) Tick(ctx context.Context, _ []portainer.PolicyID) []policyreconcile.ActualState {
	c.mu.Lock()
	handlers := make([]*HelmHandler, 0, len(c.handlers))
	for _, h := range c.handlers {
		handlers = append(handlers, h)
	}
	c.mu.Unlock()

	for _, h := range handlers {
		h.checkNamespaceDrift(ctx)
	}
	return nil
}

// checkNamespaceDrift lists the live namespace list via this handler's own
// kubeClient and, only if it has changed since the last check, re-applies
// namespaceDriftSensitiveCharts to pick up the change. Called on every poll
// (via NamespaceDriftCoordinator.Tick) rather than on its own timer.
//
// The snapshot is keyed by namespace name with ResourceVersion as the value
// (see KubeClient.ListNamespaces), and ResourceVersion is bumped by the API
// server on any mutation to the object - not just when it's created or
// deleted. So this also catches a namespace being relabeled in place (e.g. the
// io.portainer.kubernetes.namespace.system label being added or removed on an
// existing namespace), without needing to diff labels explicitly.
// installChartBundle already records success/failure on each chart's record,
// so this only needs to trigger the reapply and push the resulting statuses
// through the chart reporter, same as reconcileCharts does. lastNamespaces is
// only advanced once the reapply succeeds; on failure the snapshot is left
// stale so the next poll still sees drift and retries, instead of the failure
// going unnoticed until the namespace list happens to change again.
func (h *HelmHandler) checkNamespaceDrift(ctx context.Context) {
	if h.namespaceLister == nil {
		return
	}

	current, err := h.namespaceLister.ListNamespaces(ctx)
	if err != nil {
		log.Warn().Err(err).Str("context", "HelmPolicyHandler").Msg("Failed to list namespaces, skipping drift check")
		return
	}

	h.mu.Lock()
	changed := !maps.Equal(h.lastNamespaces, current)
	h.mu.Unlock()

	if !changed {
		return
	}

	succeeded := h.reapplyNamespaceDriftCharts(ctx)

	if h.chartReporter != nil {
		h.mu.Lock()
		statuses := buildChartStatuses(0, h.installedCharts)
		h.mu.Unlock()
		h.chartReporter.Set(h.policyID, statuses)
	}

	if succeeded {
		h.mu.Lock()
		h.lastNamespaces = current
		h.mu.Unlock()
	}
}

// namespaceDriftReapplyEligible reports whether a chart record should be
// reapplied for namespace drift. Uninstalling (policy being torn down) and
// Conflict (externally-managed release) are excluded; Installed and Failed
// are both eligible so a chart that failed a previous drift reapply keeps
// getting retried on the next detected namespace change, instead of being
// silently excluded until an unrelated fingerprint change happens to route it
// back through the normal Apply path.
func namespaceDriftReapplyEligible(rec chartRecord, ok bool) bool {
	if !ok {
		return false
	}
	switch rec.Status {
	case portainer.HelmInstallStatusUninstalling, portainer.HelmInstallStatusConflict:
		return false
	default:
		return true
	}
}

// reapplyNamespaceDriftCharts re-runs Upgrade() for every eligible chart in
// namespaceDriftSensitiveCharts, using the bundle cached from the last
// server-driven installation. A missing cached bundle (not yet installed, or
// installed before an agent restart) is skipped; it will be picked up once the
// normal Apply path installs the chart and populates the cache.
// installChartBundle records success/failure on the chart's record itself;
// the bool return is solely for checkNamespaceDrift to decide whether the
// namespace snapshot can advance — false means at least one chart failed and
// the drift is still unresolved.
func (h *HelmHandler) reapplyNamespaceDriftCharts(ctx context.Context) bool {
	h.mu.Lock()
	var toReapply []portainer.PolicyChartBundle
	for name := range namespaceDriftSensitiveCharts {
		rec, ok := h.installedCharts[name]
		if !namespaceDriftReapplyEligible(rec, ok) {
			continue
		}
		bundle, ok := h.lastBundles[name]
		if !ok {
			continue
		}
		toReapply = append(toReapply, bundle)
	}
	h.mu.Unlock()

	if len(toReapply) == 0 {
		return true
	}

	tempDir, err := os.MkdirTemp("", "helm-charts-drift-*")
	if err != nil {
		log.Warn().Err(err).Str("context", "HelmPolicyHandler").Msg("Failed to create temp dir for namespace-drift reapply")
		return false
	}
	defer func() {
		if removeErr := os.RemoveAll(tempDir); removeErr != nil {
			log.Warn().Err(removeErr).Msg("Failed to remove temporary chart directory")
		}
	}()

	h.mu.Lock()
	defer h.mu.Unlock()

	succeeded := true
	for _, bundle := range toReapply {
		// Re-check status under lock: the chart may have been dropped from the
		// policy (reconcileCharts) or the whole policy removed (Remove) in the
		// window this lock was released above for tempDir creation. Without
		// this check, installChartBundle here could resurrect a release that
		// Remove has already uninstalled.
		rec, ok := h.installedCharts[bundle.ChartName]
		if !namespaceDriftReapplyEligible(rec, ok) {
			continue
		}

		log.Debug().Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).
			Msg("Reapplying namespace-drift-sensitive chart to pick up live namespace changes")
		if err := h.installChartBundle(ctx, bundle, tempDir); err != nil {
			log.Warn().Err(err).Str("context", "HelmPolicyHandler").Str("chart", bundle.ChartName).
				Msg("Namespace-drift reapply failed, will retry on next detected change")
			succeeded = false
		}
	}
	return succeeded
}
