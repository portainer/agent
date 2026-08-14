package policyreconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	portainer "github.com/portainer/portainer/api"
)

// Status represents the current enforcement state of a policy handler.
type Status string

const (
	StatusApplying Status = "applying"
	StatusApplied  Status = "applied"
	StatusFailed   Status = "failed"
	StatusRemoving Status = "removing"
)

// DesiredState is the server's declared intent for a single policy instance.
type DesiredState struct {
	PolicyID    portainer.PolicyID `json:"policyID"`
	Type        string             `json:"type"`
	Fingerprint string             `json:"fingerprint"`
	Config      json.RawMessage    `json:"config"`
}

// ActualState is the agent's reported state for a single policy instance.
type ActualState struct {
	PolicyID    portainer.PolicyID
	Type        string
	Fingerprint string
	Status      Status
	Message     string
}

// PolicyHandler is implemented by each policy type's handler package.
// One instance is created per active policy via a HandlerFactory.
//
// The reconciler does not call Status() — it tracks lifecycle state internally
// via Reconciler.actual. Status() exists for direct handler observation (tests,
// legacy callers that hold a concrete handler reference).
type PolicyHandler interface {
	Apply(ctx context.Context, config json.RawMessage) error
	Remove(ctx context.Context) error
	Status() ActualState
}

// HandlerFactory produces a new PolicyHandler for the given policy ID.
type HandlerFactory func(policyID portainer.PolicyID) PolicyHandler

// PollHook is an optional interface implemented by handler-package coordinators
// that need cross-poll work outside the reconciler's per-cycle Apply/Remove
// lifecycle. The reconciler core never calls this — PollService dispatches to
// registered hooks each poll via Tick. See RestoreCoordinator for the canonical
// use case (helm restore retries).
type PollHook interface {
	// Tick runs cross-poll work. desired is the set of PolicyIDs currently active
	// on the agent; hooks use it to cancel work for policies that have reappeared
	// (re-creation race). Returns ActualState entries to merge into the per-policy
	// status report; nil if nothing to report.
	Tick(ctx context.Context, desired []portainer.PolicyID) []ActualState
}

// Registration bundles the factory and poll hooks for a policy type.
// It is returned by each policy type's Registration() factory function and passed
// to PollService.RegisterPolicy().
type Registration struct {
	Type      string         // Policy type identifier (e.g. "helm-k8s", "cleanup-docker").
	Factory   HandlerFactory // Creates a new handler for each active policy instance.
	PollHooks []PollHook     // Cross-poll work hooks (optional, may be nil).
}

// Reconciler is the generic per-policy reconciliation engine.

// It is helm-blind: all domain logic lives inside the registered handlers.
type Reconciler struct {
	factories map[string]HandlerFactory
	handlers  map[portainer.PolicyID]PolicyHandler
	// actual is the authoritative lifecycle state for each policy. The reconciler
	// sets it on every Apply (success or failure) and on factory-missing errors.
	// Statuses() returns these values directly — handler.Status() is not consulted.
	actual map[portainer.PolicyID]ActualState
	mu     sync.RWMutex
}

// NewReconciler constructs an empty Reconciler with no factories registered.
func NewReconciler() *Reconciler {
	return &Reconciler{
		factories: make(map[string]HandlerFactory),
		handlers:  make(map[portainer.PolicyID]PolicyHandler),
		actual:    make(map[portainer.PolicyID]ActualState),
	}
}

// RegisterFactory registers a HandlerFactory for the given policy type string.
// Handler-version warnings are computed server-side from endpoint.Agent.Version
// against LastUpdatedAgentVersion[type]; no agent-side reporting is needed.
func (r *Reconciler) RegisterFactory(policyType string, f HandlerFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[policyType] = f
}

func (r *Reconciler) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers = make(map[portainer.PolicyID]PolicyHandler)
	r.actual = make(map[portainer.PolicyID]ActualState)
}

// Reconcile drives the reconcile cycle for a single poll. It serialises:
//  1. Remove departed resource-patch policies first so restore annotations remain correct.
//  2. Apply handlers for new or changed desired states.
//  3. Remove departed handlers of every other policy type.
//
// The global lock is held for the full cycle. Two concurrent Reconcile calls
// serialise — the second blocks until the first completes. This is intentional:
// Helm installs can be slow (30-60s), serialising them avoids concurrent
// modifications to the same cluster. A future optimisation could release the
// lock between handler creation and Apply, but that is not needed today.
func (r *Reconciler) Reconcile(ctx context.Context, desired []DesiredState) {
	r.mu.Lock()
	defer r.mu.Unlock()

	desiredByID := index(desired)

	r.removeUndesired(ctx, desiredByID, func(policyType string) bool {
		return policyType == portainer.ResourcePatchAgentType
	})

	for _, d := range desired {
		handler, exists := r.handlers[d.PolicyID]
		if !exists {
			factory, ok := r.factories[d.Type]
			if !ok {
				r.actual[d.PolicyID] = ActualState{
					PolicyID: d.PolicyID,
					Type:     d.Type,
					Status:   StatusFailed,
					Message:  fmt.Sprintf("no handler registered for policy type %q", d.Type),
				}
				continue
			}
			handler = factory(d.PolicyID)
			r.handlers[d.PolicyID] = handler
		}

		if cached := r.actual[d.PolicyID]; cached.Fingerprint == d.Fingerprint && cached.Status == StatusApplied {
			continue
		}

		r.actual[d.PolicyID] = ActualState{PolicyID: d.PolicyID, Type: d.Type, Status: StatusApplying}
		if err := handler.Apply(ctx, d.Config); err != nil {
			r.actual[d.PolicyID] = ActualState{
				PolicyID:    d.PolicyID,
				Type:        d.Type,
				Fingerprint: d.Fingerprint,
				Status:      StatusFailed,
				Message:     err.Error(),
				// Fingerprint is set so the server can persist the failed status.
				// Retry is guaranteed because status != StatusApplied.
			}
		} else {
			r.actual[d.PolicyID] = ActualState{
				PolicyID:    d.PolicyID,
				Type:        d.Type,
				Fingerprint: d.Fingerprint,
				Status:      StatusApplied,
				Message:     "Successfully installed",
			}
		}
	}

	r.removeUndesired(ctx, desiredByID, func(policyType string) bool {
		return policyType != portainer.ResourcePatchAgentType
	})
}

// removeUndesired removes and discards every handler no longer present in desired
// whose policy type satisfies match. Best-effort: a handler is always discarded
// even if its Remove fails — retaining a stuck handler risks masking a clean
// re-create of the same policy ID on the next cycle.
func (r *Reconciler) removeUndesired(ctx context.Context, desiredByID map[portainer.PolicyID]struct{}, match func(policyType string) bool) {
	for id, handler := range r.handlers {
		if _, wanted := desiredByID[id]; wanted {
			continue
		}
		if !match(r.actual[id].Type) {
			continue
		}
		actual := r.actual[id]
		actual.Status = StatusRemoving
		r.actual[id] = actual
		// StatusRemoving above is set for intent documentation only — it is never
		// observable externally because Reconcile holds mu.Lock() for its full
		// duration and Statuses() requires mu.RLock(). The state is deleted below.
		_ = handler.Remove(ctx)
		delete(r.handlers, id)
		delete(r.actual, id)
	}
}

// Statuses returns a snapshot of all current actual states.
// Safe to call concurrently with Reconcile.
func (r *Reconciler) Statuses() []ActualState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ActualState, 0, len(r.actual))
	for _, s := range r.actual {
		out = append(out, s)
	}
	return out
}

func index(desired []DesiredState) map[portainer.PolicyID]struct{} {
	out := make(map[portainer.PolicyID]struct{}, len(desired))
	for _, d := range desired {
		out[d.PolicyID] = struct{}{}
	}
	return out
}
