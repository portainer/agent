package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// RestoreAnnotation holds a JSON snapshot of the values a resource carried before the patcher.
const RestoreAnnotation = "resource-patch.portainer.io/restore"

type ResourceTarget struct {
	APIVersion string
	Kind       string
	Resource   string
	Name       string
	Namespace  string
}

// restoreSnapshot is the sectioned restore-annotation payload, keyed by dotted field path.
type restoreSnapshot struct {
	Fields map[string]map[string]*json.RawMessage `json:"fields,omitempty"`
}

// ApplyManagedField sets every key in values at fieldPath, snapshotting prior values for restore.
// owns scopes only the destructive cleanup: exclusive mode wipes owned keys the caller dropped,
// additive mode prunes keys it previously set, and restore reverts owned keys. Callers must own
// every key they set, or it will leak on detach. Creates a missing target iff createIfMissing.
// An empty fieldPath targets the object root. Idempotent.
func (kcl *KubeClient) ApplyManagedField(ctx context.Context, target ResourceTarget, fieldPath []string, values map[string][]byte, owns func(key string) bool, exclusive, createIfMissing bool) error {
	desired, err := decodeValues(values)
	if err != nil {
		return fmt.Errorf("decode values for %s %q: %w", target.Kind, target.Name, err)
	}

	gvr, err := targetGVR(target)
	if err != nil {
		return err
	}

	obj, err := kcl.dynamicResource(gvr, target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if !createIfMissing {
			return fmt.Errorf("%s %q not found", target.Kind, target.Name)
		}
		return kcl.createWithManagedField(ctx, target, gvr, fieldPath, desired)
	}
	if err != nil {
		return fmt.Errorf("get %s %q: %w", target.Kind, target.Name, err)
	}

	current := readMapAtPath(obj.Object, fieldPath)
	snapshot, _, err := decodeRestoreSnapshot(obj.GetAnnotations())
	if err != nil {
		return fmt.Errorf("decode restore annotation on %s %q: %w", target.Kind, target.Name, err)
	}
	section := sectionKey(fieldPath)
	snapshot.section(section)

	fieldPatch := map[string]any{}
	for key, value := range desired {
		if !valueEqual(current[key], value) {
			fieldPatch[key] = value
		}
		recordOnce(snapshot.Fields[section], key, current)
	}
	if exclusive {
		for key := range current {
			if owns(key) && !hasKey(desired, key) {
				fieldPatch[key] = nil // owned sibling the policy omits → remove
				recordOnce(snapshot.Fields[section], key, current)
			}
		}
	} else {
		for key := range snapshot.Fields[section] {
			if owns(key) && !hasKey(desired, key) && hasKey(current, key) {
				fieldPatch[key] = nil // key we previously set but no longer want → remove
			}
		}
	}

	encoded, err := encodeRestoreSnapshot(snapshot)
	if err != nil {
		return err
	}
	if len(fieldPatch) == 0 && obj.GetAnnotations()[RestoreAnnotation] == encoded {
		return nil // already converged
	}

	return kcl.patch(ctx, gvr, target, fieldPath, fieldPatch, &encoded)
}

// RestoreManagedField reverts the caller's owned keys at fieldPath to their prior values (keys
// absent before the first apply are deleted), removes just those keys from the snapshot, and
// drops the annotation only when it becomes empty. No-op if the resource/annotation is absent.
func (kcl *KubeClient) RestoreManagedField(ctx context.Context, target ResourceTarget, fieldPath []string, owns func(key string) bool) error {
	gvr, err := targetGVR(target)
	if err != nil {
		return err
	}

	obj, err := kcl.dynamicResource(gvr, target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get %s %q: %w", target.Kind, target.Name, err)
	}

	snapshot, present, err := decodeRestoreSnapshot(obj.GetAnnotations())
	if err != nil {
		return fmt.Errorf("decode restore annotation on %s %q: %w", target.Kind, target.Name, err)
	}
	if !present {
		return nil
	}
	section := sectionKey(fieldPath)
	keys := snapshot.Fields[section]

	fieldPatch := map[string]any{}
	for key, prior := range keys {
		if !owns(key) {
			continue
		}
		if prior == nil {
			fieldPatch[key] = nil // absent before the patch → remove
		} else {
			fieldPatch[key] = *prior
		}
		delete(keys, key)
	}
	if len(fieldPatch) == 0 {
		return nil
	}
	if len(keys) == 0 {
		delete(snapshot.Fields, section)
	}

	var encoded *string
	if len(snapshot.Fields) > 0 {
		e, err := encodeRestoreSnapshot(snapshot)
		if err != nil {
			return err
		}
		encoded = &e
	}

	return kcl.patch(ctx, gvr, target, fieldPath, fieldPatch, encoded)
}

// patch issues a single merge patch mutating the keys at fieldPath and either setting
// (encoded != nil) or removing (encoded == nil) the restore annotation.
func (kcl *KubeClient) patch(ctx context.Context, gvr schema.GroupVersionResource, target ResourceTarget, fieldPath []string, fieldPatch map[string]any, encoded *string) error {
	root := map[string]any{}
	for key, value := range fieldPatch {
		setNested(root, append(slices.Clone(fieldPath), key), value)
	}
	if encoded != nil {
		setNested(root, []string{"metadata", "annotations", RestoreAnnotation}, *encoded)
	} else {
		setNested(root, []string{"metadata", "annotations", RestoreAnnotation}, nil)
	}

	body, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal patch for %s %q: %w", target.Kind, target.Name, err)
	}
	if _, err := kcl.dynamicResource(gvr, target.Namespace).Patch(ctx, target.Name, types.MergePatchType, body, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch %s %q: %w", target.Kind, target.Name, err)
	}
	return nil
}

// createWithManagedField creates a minimal resource carrying the desired values at fieldPath and
// a restore annotation snapshotting every desired key as previously-absent.
func (kcl *KubeClient) createWithManagedField(ctx context.Context, target ResourceTarget, gvr schema.GroupVersionResource, fieldPath []string, desired map[string]any) error {
	snapshot := restoreSnapshot{Fields: map[string]map[string]*json.RawMessage{}}
	section := snapshot.section(sectionKey(fieldPath))
	for key := range desired {
		section[key] = nil
	}
	encoded, err := encodeRestoreSnapshot(snapshot)
	if err != nil {
		return err
	}

	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetAPIVersion(target.APIVersion)
	obj.SetKind(target.Kind)
	obj.SetName(target.Name)
	if target.Namespace != "" {
		obj.SetNamespace(target.Namespace)
	}
	for key, value := range desired {
		setNested(obj.Object, append(slices.Clone(fieldPath), key), value)
	}
	setNested(obj.Object, []string{"metadata", "annotations", RestoreAnnotation}, encoded)

	if _, err := kcl.dynamicResource(gvr, target.Namespace).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create %s %q: %w", target.Kind, target.Name, err)
	}
	return nil
}

func (kcl *KubeClient) dynamicResource(gvr schema.GroupVersionResource, namespace string) dynamic.ResourceInterface {
	if namespace == "" {
		return kcl.dynamicCli.Resource(gvr)
	}
	return kcl.dynamicCli.Resource(gvr).Namespace(namespace)
}

func targetGVR(target ResourceTarget) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(target.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("parse apiVersion %q: %w", target.APIVersion, err)
	}
	return gv.WithResource(target.Resource), nil
}

// readMapAtPath returns a copy of the string-keyed map at fieldPath, or an empty map if absent.
// An empty fieldPath returns the object root.
func readMapAtPath(obj map[string]any, fieldPath []string) map[string]any {
	if len(fieldPath) == 0 {
		return obj
	}
	m, found, err := unstructured.NestedMap(obj, fieldPath...)
	if err != nil || !found {
		return map[string]any{}
	}
	return m
}

// setNested sets value at path, creating intermediate maps. A nil value produces a JSON null,
// which deletes the key under a merge patch.
func setNested(root map[string]any, path []string, value any) {
	m := root
	for _, p := range path[:len(path)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[path[len(path)-1]] = value
}

// recordOnce stores key's prior value the first time it is touched; a key absent from current is
// recorded as nil so a restore knows to delete it. Existing entries are never overwritten.
func recordOnce(section map[string]*json.RawMessage, key string, current map[string]any) {
	if _, seen := section[key]; seen {
		return
	}
	value, ok := current[key]
	if !ok {
		section[key] = nil
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		section[key] = nil
		return
	}
	msg := json.RawMessage(raw)
	section[key] = &msg
}

func decodeValues(values map[string][]byte) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for key, raw := range values {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("value for key %q: %w", key, err)
		}
		out[key] = v
	}
	return out, nil
}

// valueEqual reports whether current equals desired, comparing by canonical JSON.
func valueEqual(current, desired any) bool {
	a, err := json.Marshal(current)
	if err != nil {
		return false
	}
	b, err := json.Marshal(desired)
	if err != nil {
		return false
	}
	return string(a) == string(b)
}

func hasKey[V any](m map[string]V, key string) bool {
	_, ok := m[key]
	return ok
}

func sectionKey(fieldPath []string) string {
	return strings.Join(fieldPath, ".")
}

// section returns the snapshot section for key, allocating it (and Fields) if needed.
func (s *restoreSnapshot) section(key string) map[string]*json.RawMessage {
	if s.Fields == nil {
		s.Fields = map[string]map[string]*json.RawMessage{}
	}
	if s.Fields[key] == nil {
		s.Fields[key] = map[string]*json.RawMessage{}
	}
	return s.Fields[key]
}

// decodeRestoreSnapshot reads the restore annotation. The bool reports whether it was present.
func decodeRestoreSnapshot(annotations map[string]string) (restoreSnapshot, bool, error) {
	raw, ok := annotations[RestoreAnnotation]
	if !ok {
		return restoreSnapshot{}, false, nil
	}
	var snapshot restoreSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return restoreSnapshot{}, true, err
	}
	return snapshot, true, nil
}

func encodeRestoreSnapshot(snapshot restoreSnapshot) (string, error) {
	body, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("marshal restore snapshot: %w", err)
	}
	return string(body), nil
}
