package policyreconcile_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/portainer/agent/policyreconcile"
)

// stubHandler is a minimal PolicyHandler for testing.
type stubHandler struct {
	mu          sync.Mutex
	applyCalls  int
	removeCalls int
	applyErr    error
	removeErr   error
	lastConfig  json.RawMessage
	policyID    portainer.PolicyID
	status      policyreconcile.ActualState
}

func (h *stubHandler) Apply(_ context.Context, config json.RawMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.applyCalls++
	h.lastConfig = config
	h.status = policyreconcile.ActualState{
		PolicyID: h.policyID,
		Status:   policyreconcile.StatusApplied,
	}
	if h.applyErr != nil {
		h.status = policyreconcile.ActualState{
			PolicyID: h.policyID,
			Status:   policyreconcile.StatusFailed,
			Message:  h.applyErr.Error(),
		}
	}
	return h.applyErr
}

func (h *stubHandler) Remove(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeCalls++
	h.status.Status = policyreconcile.StatusRemoving
	return h.removeErr
}

func (h *stubHandler) Status() policyreconcile.ActualState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

func newFactory(handlers map[portainer.PolicyID]*stubHandler, applyErr error) policyreconcile.HandlerFactory {
	return func(id portainer.PolicyID) policyreconcile.PolicyHandler {
		h := &stubHandler{policyID: id, applyErr: applyErr}
		handlers[id] = h
		return h
	}
}

// orderingHandler records the sequence of Apply/Remove calls across all handlers
// into a shared log, so tests can assert cross-handler ordering within a cycle.
type orderingHandler struct {
	id     portainer.PolicyID
	record func(string)
}

func (h *orderingHandler) Apply(context.Context, json.RawMessage) error {
	h.record(fmt.Sprintf("apply-%d", h.id))
	return nil
}

func (h *orderingHandler) Remove(context.Context) error {
	h.record(fmt.Sprintf("remove-%d", h.id))
	return nil
}

func (h *orderingHandler) Status() policyreconcile.ActualState {
	return policyreconcile.ActualState{PolicyID: h.id, Status: policyreconcile.StatusApplied}
}

func orderingFactory(record func(string)) policyreconcile.HandlerFactory {
	return func(id portainer.PolicyID) policyreconcile.PolicyHandler {
		return &orderingHandler{id: id, record: record}
	}
}

// A superseding resource-patch policy must apply AFTER the departed one is removed,
// so the outgoing policy's owner-scoped restore does not clobber the incoming values.
func TestReconcile_ResourcePatchRemovedBeforeApply(t *testing.T) {
	var (
		mu     sync.Mutex
		events []string
	)
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, s)
	}

	r := policyreconcile.NewReconciler()
	r.RegisterFactory(portainer.ResourcePatchAgentType, orderingFactory(record))

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: portainer.ResourcePatchAgentType, Fingerprint: "a", Config: json.RawMessage(`{}`)},
	})
	// Policy 2 supersedes policy 1: 1 departs, 2 is new — in the same cycle.
	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 2, Type: portainer.ResourcePatchAgentType, Fingerprint: "b", Config: json.RawMessage(`{}`)},
	})

	assert.Equal(t, []string{"apply-1", "remove-1", "apply-2"}, events)
}

// Non-resource-patch policy types keep the default apply-before-remove ordering.
func TestReconcile_OtherTypeRemovedAfterApply(t *testing.T) {
	var (
		mu     sync.Mutex
		events []string
	)
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, s)
	}

	r := policyreconcile.NewReconciler()
	r.RegisterFactory("helm-k8s", orderingFactory(record))

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "helm-k8s", Fingerprint: "a", Config: json.RawMessage(`{}`)},
	})
	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 2, Type: "helm-k8s", Fingerprint: "b", Config: json.RawMessage(`{}`)},
	})

	assert.Equal(t, []string{"apply-1", "apply-2", "remove-1"}, events)
}

func TestReconcile_NewPolicy_FactoryCalledAndApplied(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("test", newFactory(handlers, nil))

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	})

	require.Contains(t, handlers, portainer.PolicyID(1))
	assert.Equal(t, 1, handlers[1].applyCalls)
	statuses := r.Statuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, policyreconcile.StatusApplied, statuses[0].Status)
	assert.Equal(t, "fp1", statuses[0].Fingerprint)
}

func TestReconcile_SameFingerprintApplied_NoOp(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("test", newFactory(handlers, nil))

	desired := []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	}
	r.Reconcile(context.Background(), desired)
	r.Reconcile(context.Background(), desired) // second call, same fingerprint

	assert.Equal(t, 1, handlers[1].applyCalls, "Apply should only be called once")
}

func TestReconcile_ChangedFingerprint_ApplyCalledOnExistingHandler(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("test", newFactory(handlers, nil))

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	})
	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp2", Config: json.RawMessage(`{}`)},
	})

	assert.Equal(t, 2, handlers[1].applyCalls, "Apply should be called again on fingerprint change")
	assert.Len(t, handlers, 1, "Should reuse the same handler instance")
}

func TestReconcile_PolicyRemoved_RemoveCalled(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("test", newFactory(handlers, nil))

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	})
	r.Reconcile(context.Background(), nil) // policy absent from desired

	assert.Equal(t, 1, handlers[1].removeCalls)
	assert.Empty(t, r.Statuses(), "Actual state should be cleaned up after Remove")
}

func TestReconcile_ApplyError_FingerprintPreservedStatusFailed(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("test", newFactory(handlers, errors.New("install failed")))

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	})

	statuses := r.Statuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, policyreconcile.StatusFailed, statuses[0].Status)
	assert.Contains(t, statuses[0].Message, "install failed")
	// Fingerprint is preserved so the server can persist the failed status.
	// Retry is guaranteed because status != StatusApplied.
	assert.Equal(t, "fp1", statuses[0].Fingerprint,
		"Fingerprint should be preserved on failure so server can persist the failed status")
}

func TestReconcile_FailedPolicy_RetriedOnNextCycle(t *testing.T) {
	t.Parallel()
	// Verify that a failed policy IS retried on the next reconcile cycle
	// even though the fingerprint is preserved (retry driven by status != Applied).
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}

	factory := func(id portainer.PolicyID) policyreconcile.PolicyHandler {
		h := &stubHandler{
			policyID: id,
			applyErr: errors.New("transient failure"),
		}
		handlers[id] = h
		return h
	}
	r.RegisterFactory("test", factory)

	desired := []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	}

	// First cycle: fails.
	r.Reconcile(context.Background(), desired)
	assert.Equal(t, 1, handlers[1].applyCalls)

	// Second cycle: same fingerprint, still failed → must retry.
	handlers[1].applyErr = nil // succeed this time
	r.Reconcile(context.Background(), desired)
	assert.Equal(t, 2, handlers[1].applyCalls,
		"Failed policy must be retried even with same fingerprint")

	statuses := r.Statuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, policyreconcile.StatusApplied, statuses[0].Status)
}

func TestReconcile_RemoveError_HandlerStillDiscarded(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	factory := func(id portainer.PolicyID) policyreconcile.PolicyHandler {
		h := &stubHandler{policyID: id, removeErr: errors.New("uninstall failed")}
		handlers[id] = h
		return h
	}
	r.RegisterFactory("test", factory)

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "test", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	})
	r.Reconcile(context.Background(), nil)

	assert.Equal(t, 1, handlers[1].removeCalls)
	assert.Empty(t, r.Statuses(), "Handler must be discarded even when Remove errors")
}

func TestReconcile_UnknownType_StatusFailed(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "unknown-type", Fingerprint: "fp1", Config: json.RawMessage(`{}`)},
	})

	statuses := r.Statuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, policyreconcile.StatusFailed, statuses[0].Status)
	assert.Contains(t, statuses[0].Message, "no handler registered")
}

func TestReconcile_ConcurrentCalls_Serialize(t *testing.T) {
	t.Parallel()
	// Both goroutines reconcile the same policy with different fingerprints.
	// Because Reconcile serialises, each call must complete fully — total
	// applyCalls must equal 2 (no call is dropped or double-counted).
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("test", newFactory(handlers, nil))

	var wg sync.WaitGroup
	wg.Add(2)
	fps := []string{"fp1", "fp2"}
	for _, fp := range fps {
		go func() {
			defer wg.Done()
			r.Reconcile(context.Background(), []policyreconcile.DesiredState{
				{PolicyID: 1, Type: "test", Fingerprint: fp, Config: json.RawMessage(`{}`)},
			})
		}()
	}
	wg.Wait()

	require.Contains(t, handlers, portainer.PolicyID(1))
	assert.Equal(t, 2, handlers[1].applyCalls, "Each Reconcile call must Apply exactly once (serialised, not dropped)")
}

func TestReconcile_TwoPolicies_ModifyingOneDoesNotAffectOther(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("helm-k8s", newFactory(handlers, nil))

	cfg := json.RawMessage(`{}`)

	// Both policies applied initially.
	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "helm-k8s", Fingerprint: "fp1a", Config: cfg},
		{PolicyID: 2, Type: "helm-k8s", Fingerprint: "fp2a", Config: cfg},
	})
	require.Equal(t, 1, handlers[1].applyCalls)
	require.Equal(t, 1, handlers[2].applyCalls)

	// Only policy 1 fingerprint changes.
	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "helm-k8s", Fingerprint: "fp1b", Config: cfg},
		{PolicyID: 2, Type: "helm-k8s", Fingerprint: "fp2a", Config: cfg},
	})
	assert.Equal(t, 2, handlers[1].applyCalls, "policy 1 must re-apply on fingerprint change")
	assert.Equal(t, 1, handlers[2].applyCalls, "policy 2 must not re-apply when its fingerprint is unchanged")
	assert.Len(t, r.Statuses(), 2, "both policies still active")
}

func TestReconcile_PolicyRemoval_OnlyThatHandlerRemoved(t *testing.T) {
	t.Parallel()
	r := policyreconcile.NewReconciler()
	handlers := map[portainer.PolicyID]*stubHandler{}
	r.RegisterFactory("helm-k8s", newFactory(handlers, nil))

	cfg := json.RawMessage(`{}`)

	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 1, Type: "helm-k8s", Fingerprint: "fp1", Config: cfg},
		{PolicyID: 2, Type: "helm-k8s", Fingerprint: "fp2", Config: cfg},
	})

	// Remove only policy 1.
	r.Reconcile(context.Background(), []policyreconcile.DesiredState{
		{PolicyID: 2, Type: "helm-k8s", Fingerprint: "fp2", Config: cfg},
	})

	assert.Equal(t, 1, handlers[1].removeCalls, "policy 1 must be removed")
	assert.Equal(t, 0, handlers[2].removeCalls, "policy 2 must NOT be removed")
	statuses := r.Statuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, portainer.PolicyID(2), statuses[0].PolicyID, "only policy 2 remains in actual state")
}
