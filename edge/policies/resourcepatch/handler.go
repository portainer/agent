// Package resourcepatch implements the agent-side executor for resource-patch-k8s policies:
// it applies the server's field-patch operations authoritatively and reverses them on detach.
// See portainer.ResourcePatchAgentType for the design rationale.
package resourcepatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/libpolicy"
	"github.com/rs/zerolog/log"

	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/policyreconcile"
)

const handlerContext = "ResourcePatchHandler"

// Config is the wire format the server sends in PolicyDesiredState.Config.
type Config = portainer.ResourcePatchConfig

// fieldPatcher is the subset of the Kubernetes client the handler relies on.
// Implemented by *kubernetes.KubeClient.
type fieldPatcher interface {
	ApplyManagedField(ctx context.Context, target kubernetes.ResourceTarget, fieldPath []string, values map[string][]byte, owns func(key string) bool, exclusive, createIfMissing bool) error
	RestoreManagedField(ctx context.Context, target kubernetes.ResourceTarget, fieldPath []string, owns func(key string) bool) error
}

// applied records a patched field and its ownership scope, so restore is owner-scoped.
type applied struct {
	target           kubernetes.ResourceTarget
	fieldPath        []string
	ownedKeyPrefixes []string
}

func (a applied) key() string {
	target := a.target
	prefixes := slices.Sorted(slices.Values(a.ownedKeyPrefixes))
	return strings.Join([]string{target.APIVersion, target.Kind, target.Resource, target.Namespace, target.Name, strings.Join(a.fieldPath, "."), strings.Join(prefixes, ",")}, "|")
}

// Registration returns a policyreconcile.Registration for resource-patch-k8s policies.
// Call this from edge.go inside the Kubernetes platform guard.
func Registration(kube *kubernetes.KubeClient) policyreconcile.Registration {
	log.Debug().
		Str("type", portainer.ResourcePatchAgentType).
		Str("context", handlerContext).
		Msg("Registering resource-patch-k8s policy reconciler")

	return policyreconcile.Registration{
		Type:    portainer.ResourcePatchAgentType,
		Factory: NewHandler(kube),
	}
}

// Handler implements policyreconcile.PolicyHandler for resource-patch-k8s policies.
// One instance is created per active policy ID by the factory returned from NewHandler.
type Handler struct {
	policyID portainer.PolicyID
	patcher  fieldPatcher

	mu          sync.Mutex
	applied     []applied // fields currently carrying this policy's values
	fingerprint string
}

// NewHandler returns a HandlerFactory for resource-patch-k8s policies.
func NewHandler(patcher fieldPatcher) policyreconcile.HandlerFactory {
	return func(policyID portainer.PolicyID) policyreconcile.PolicyHandler {
		return &Handler{policyID: policyID, patcher: patcher}
	}
}

// Apply decodes the patch operations and applies each authoritatively via the dynamic client.
// Fields dropped from the config since the last apply are restored to their pre-policy values.
func (h *Handler) Apply(ctx context.Context, raw json.RawMessage) error {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("decode resource patch config: %w", err)
	}

	desired := make([]applied, 0, len(cfg.Patches))
	for _, op := range cfg.Patches {
		if op.APIVersion == "" || op.Kind == "" || op.Resource == "" || op.Name == "" {
			return errors.New("patch operation requires apiVersion, kind, resource and name")
		}
		desired = append(desired, applied{target: target(op), fieldPath: op.FieldPath, ownedKeyPrefixes: op.OwnedKeyPrefixes})
	}

	// Restore fields removed from the config since the last apply.
	for _, a := range h.removed(desired) {
		if err := h.patcher.RestoreManagedField(ctx, a.target, a.fieldPath, prefixOwns(a.ownedKeyPrefixes)); err != nil {
			return fmt.Errorf("restore %s %q: %w", a.target.Kind, a.target.Name, err)
		}
	}

	for _, op := range cfg.Patches {
		if err := h.patcher.ApplyManagedField(ctx, target(op), op.FieldPath, rawValues(op.Values), prefixOwns(op.OwnedKeyPrefixes), op.Exclusive, op.CreateIfMissing); err != nil {
			return fmt.Errorf("apply %s %q: %w", op.Kind, op.Name, err)
		}
	}

	h.mu.Lock()
	h.applied = desired
	h.fingerprint = libpolicy.ConfigFingerprint(raw)
	h.mu.Unlock()

	log.Debug().
		Int("policy_id", int(h.policyID)).
		Int("patches", len(cfg.Patches)).
		Str("context", handlerContext).
		Msg("Applied resource-patch-k8s policy")

	return nil
}

// Remove restores every field this policy patched to its pre-policy state.
func (h *Handler) Remove(ctx context.Context) error {
	h.mu.Lock()
	snapshot := slices.Clone(h.applied)
	h.mu.Unlock()

	var errs []error
	for _, a := range snapshot {
		if err := h.patcher.RestoreManagedField(ctx, a.target, a.fieldPath, prefixOwns(a.ownedKeyPrefixes)); err != nil {
			errs = append(errs, fmt.Errorf("restore %s %q: %w", a.target.Kind, a.target.Name, err))
		}
	}

	h.mu.Lock()
	h.applied = nil
	h.mu.Unlock()

	log.Debug().
		Int("policy_id", int(h.policyID)).
		Str("context", handlerContext).
		Msg("Removed resource-patch-k8s policy")

	return errors.Join(errs...)
}

// Status returns the handler's current state for direct observation. The reconciler tracks
// the authoritative status itself and does not call this.
func (h *Handler) Status() policyreconcile.ActualState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return policyreconcile.ActualState{
		PolicyID:    h.policyID,
		Type:        portainer.ResourcePatchAgentType,
		Fingerprint: h.fingerprint,
		Status:      policyreconcile.StatusApplied,
	}
}

// removed returns entries present in the last apply but absent from desired.
func (h *Handler) removed(desired []applied) []applied {
	desiredKeys := make(map[string]struct{}, len(desired))
	for _, a := range desired {
		desiredKeys[a.key()] = struct{}{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var out []applied
	for _, a := range h.applied {
		if _, keep := desiredKeys[a.key()]; !keep {
			out = append(out, a)
		}
	}
	return out
}

func target(op portainer.ResourcePatchOperation) kubernetes.ResourceTarget {
	return kubernetes.ResourceTarget{
		APIVersion: op.APIVersion,
		Kind:       op.Kind,
		Resource:   op.Resource,
		Name:       op.Name,
		Namespace:  op.Namespace,
	}
}

// rawValues converts the wire Values to the raw-bytes map the patcher expects.
func rawValues(values map[string]json.RawMessage) map[string][]byte {
	out := make(map[string][]byte, len(values))
	for key, v := range values {
		out[key] = []byte(v)
	}
	return out
}

// prefixOwns builds an ownership predicate matching any key with one of the given prefixes.
func prefixOwns(prefixes []string) func(key string) bool {
	return func(key string) bool {
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
		return false
	}
}
