package kubernetes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

const psaPrefix = "pod-security.kubernetes.io/"

func psaOwns(key string) bool { return strings.HasPrefix(key, psaPrefix) }

func exactOwns(want string) func(string) bool {
	return func(key string) bool { return key == want }
}

var labelsPath = []string{"metadata", "labels"}

func newPatchClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClient(scheme.Scheme, objs...)
}

func vals(t *testing.T, m map[string]any) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(m))
	for k, v := range m {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		out[k] = b
	}
	return out
}

func getObj(t *testing.T, kcl *KubeClient, target ResourceTarget) *unstructured.Unstructured {
	t.Helper()
	gvr, err := targetGVR(target)
	require.NoError(t, err)
	u, err := kcl.dynamicResource(gvr, target.Namespace).Get(context.Background(), target.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return u
}

func restoreSection(t *testing.T, u *unstructured.Unstructured, section string) map[string]*json.RawMessage {
	t.Helper()
	raw, ok := u.GetAnnotations()[RestoreAnnotation]
	require.True(t, ok, "restore annotation must be present")
	var snap restoreSnapshot
	require.NoError(t, json.Unmarshal([]byte(raw), &snap))
	return snap.Fields[section]
}

func nsTarget(name string) ResourceTarget {
	return ResourceTarget{APIVersion: "v1", Kind: "Namespace", Resource: "namespaces", Name: name}
}

func TestApplyManagedField_MapExclusive_CreatesMissing(t *testing.T) {
	kcl := &KubeClient{dynamicCli: newPatchClient()}

	require.NoError(t, kcl.ApplyManagedField(context.Background(), nsTarget("team-a"), labelsPath,
		vals(t, map[string]any{psaPrefix + "enforce": "baseline"}), psaOwns, true, true))

	u := getObj(t, kcl, nsTarget("team-a"))
	assert.Equal(t, "baseline", u.GetLabels()[psaPrefix+"enforce"])
	assert.Nil(t, restoreSection(t, u, "metadata.labels")[psaPrefix+"enforce"], "created key recorded as previously-absent")
}

func TestApplyManagedField_MapExclusive_ReplaceWipeAndSnapshot(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{
		psaPrefix + "enforce": "privileged", // overwritten
		psaPrefix + "warn":    "baseline",   // owned, omitted → wiped
		"team":                "payments",   // not owned → untouched
	}}}
	kcl := &KubeClient{dynamicCli: newPatchClient(ns)}

	require.NoError(t, kcl.ApplyManagedField(context.Background(), nsTarget("team-a"), labelsPath,
		vals(t, map[string]any{psaPrefix + "enforce": "restricted"}), psaOwns, true, true))

	u := getObj(t, kcl, nsTarget("team-a"))
	assert.Equal(t, "restricted", u.GetLabels()[psaPrefix+"enforce"])
	assert.NotContains(t, u.GetLabels(), psaPrefix+"warn", "owned-but-omitted label wiped")
	assert.Equal(t, "payments", u.GetLabels()["team"], "non-owned label untouched")

	section := restoreSection(t, u, "metadata.labels")
	assert.JSONEq(t, `"privileged"`, string(*section[psaPrefix+"enforce"]))
	assert.JSONEq(t, `"baseline"`, string(*section[psaPrefix+"warn"]))
}

func TestApplyManagedField_RecordOnceAcrossConfigChange(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{psaPrefix + "enforce": "privileged"}}}
	kcl := &KubeClient{dynamicCli: newPatchClient(ns)}
	ctx := context.Background()

	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath, vals(t, map[string]any{psaPrefix + "enforce": "baseline"}), psaOwns, true, true))
	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath, vals(t, map[string]any{psaPrefix + "enforce": "restricted", psaPrefix + "audit": "baseline"}), psaOwns, true, true))

	section := restoreSection(t, getObj(t, kcl, nsTarget("team-a")), "metadata.labels")
	assert.JSONEq(t, `"privileged"`, string(*section[psaPrefix+"enforce"]), "original preserved")
	assert.Nil(t, section[psaPrefix+"audit"], "new key recorded as previously-absent")
}

func TestApplyManagedField_IdempotentNoPatch(t *testing.T) {
	dyn := newPatchClient()
	kcl := &KubeClient{dynamicCli: dyn}
	ctx := context.Background()

	desired := vals(t, map[string]any{psaPrefix + "enforce": "baseline"})
	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath, desired, psaOwns, true, true))

	dyn.ClearActions()
	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath, desired, psaOwns, true, true))
	for _, a := range dyn.Actions() {
		assert.NotEqual(t, "patch", a.GetVerb(), "converged re-apply must not patch")
	}
}

func TestRestoreManagedField_MapExclusive(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{
		psaPrefix + "enforce": "privileged",
		"team":                "payments",
	}}}
	kcl := &KubeClient{dynamicCli: newPatchClient(ns)}
	ctx := context.Background()

	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath,
		vals(t, map[string]any{psaPrefix + "enforce": "restricted", psaPrefix + "warn": "restricted"}), psaOwns, true, true))
	require.NoError(t, kcl.RestoreManagedField(ctx, nsTarget("team-a"), labelsPath, psaOwns))

	u := getObj(t, kcl, nsTarget("team-a"))
	assert.Equal(t, "privileged", u.GetLabels()[psaPrefix+"enforce"], "prior restored")
	assert.NotContains(t, u.GetLabels(), psaPrefix+"warn", "policy-introduced label removed")
	assert.Equal(t, "payments", u.GetLabels()["team"])
	assert.NotContains(t, u.GetAnnotations(), RestoreAnnotation, "annotation dropped once empty")
}

func TestApplyRestore_MultiOwnerScoped(t *testing.T) {
	const other = "example.com/"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{
		psaPrefix + "enforce": "privileged",
		other + "team":        "old",
	}}}
	kcl := &KubeClient{dynamicCli: newPatchClient(ns)}
	ctx := context.Background()

	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath, vals(t, map[string]any{psaPrefix + "enforce": "baseline"}), psaOwns, true, true))
	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("team-a"), labelsPath, vals(t, map[string]any{other + "team": "new"}), exactOwns(other+"team"), false, true))

	require.NoError(t, kcl.RestoreManagedField(ctx, nsTarget("team-a"), labelsPath, psaOwns))
	u := getObj(t, kcl, nsTarget("team-a"))
	assert.Equal(t, "privileged", u.GetLabels()[psaPrefix+"enforce"])
	assert.Equal(t, "new", u.GetLabels()[other+"team"], "owner B untouched")
	assert.Contains(t, u.GetAnnotations(), RestoreAnnotation, "annotation survives for owner B")

	require.NoError(t, kcl.RestoreManagedField(ctx, nsTarget("team-a"), labelsPath, exactOwns(other+"team")))
	u = getObj(t, kcl, nsTarget("team-a"))
	assert.Equal(t, "old", u.GetLabels()[other+"team"])
	assert.NotContains(t, u.GetAnnotations(), RestoreAnnotation, "annotation dropped once last owner leaves")
}

func TestApplyManagedField_ConfigUpdateKeepsAnnotationAndRestoresOriginal(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{
		psaPrefix + "audit":   "baseline", // pre-existing distro labels
		psaPrefix + "enforce": "baseline",
	}}}
	kcl := &KubeClient{dynamicCli: newPatchClient(ns)}
	ctx := context.Background()

	// create with modes [enforce, warn]
	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("a"), labelsPath,
		vals(t, map[string]any{psaPrefix + "enforce": "baseline", psaPrefix + "warn": "baseline"}), psaOwns, true, true))
	require.Contains(t, getObj(t, kcl, nsTarget("a")).GetAnnotations(), RestoreAnnotation)

	// update to modes [audit, enforce].
	require.NoError(t, kcl.ApplyManagedField(ctx, nsTarget("a"), labelsPath,
		vals(t, map[string]any{psaPrefix + "audit": "baseline", psaPrefix + "enforce": "baseline"}), psaOwns, true, true))

	u := getObj(t, kcl, nsTarget("a"))
	require.Contains(t, u.GetAnnotations(), RestoreAnnotation, "annotation must survive the config update")
	section := restoreSection(t, u, "metadata.labels")
	assert.JSONEq(t, `"baseline"`, string(*section[psaPrefix+"audit"]), "original audit preserved")
	assert.JSONEq(t, `"baseline"`, string(*section[psaPrefix+"enforce"]), "original enforce preserved")
	assert.Nil(t, section[psaPrefix+"warn"], "warn recorded as previously-absent")

	// Detach must restore the two original distro labels exactly.
	require.NoError(t, kcl.RestoreManagedField(ctx, nsTarget("a"), labelsPath, psaOwns))
	u = getObj(t, kcl, nsTarget("a"))
	assert.Equal(t, map[string]string{psaPrefix + "audit": "baseline", psaPrefix + "enforce": "baseline"}, u.GetLabels())
	assert.NotContains(t, u.GetAnnotations(), RestoreAnnotation, "annotation dropped after detach")
}

func TestApplyManagedField_MapAdditive_LeavesSiblings(t *testing.T) {
	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "compute", Namespace: "team-a"},
		Spec:       corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{"limits.memory": mustQuantity("4Gi")}},
	}
	kcl := &KubeClient{dynamicCli: newPatchClient(rq)}
	target := ResourceTarget{APIVersion: "v1", Kind: "ResourceQuota", Resource: "resourcequotas", Name: "compute", Namespace: "team-a"}
	ctx := context.Background()

	require.NoError(t, kcl.ApplyManagedField(ctx, target, []string{"spec", "hard"},
		vals(t, map[string]any{"limits.cpu": "7600m"}), exactOwns("limits.cpu"), false, false))

	hard, _, err := unstructured.NestedStringMap(getObj(t, kcl, target).Object, "spec", "hard")
	require.NoError(t, err)
	assert.Equal(t, "7600m", hard["limits.cpu"], "own key set")
	assert.Equal(t, "4Gi", hard["limits.memory"], "sibling quota key left untouched")

	require.NoError(t, kcl.RestoreManagedField(ctx, target, []string{"spec", "hard"}, exactOwns("limits.cpu")))
	hard, _, err = unstructured.NestedStringMap(getObj(t, kcl, target).Object, "spec", "hard")
	require.NoError(t, err)
	assert.NotContains(t, hard, "limits.cpu", "own key removed on restore (was absent before)")
	assert.Equal(t, "4Gi", hard["limits.memory"])
}

func TestApplyManagedField_ScalarAdditive_RootField(t *testing.T) {
	sc := &storagev1.StorageClass{
		ObjectMeta:           metav1.ObjectMeta{Name: "standard"},
		Provisioner:          "example.com/provisioner",
		AllowVolumeExpansion: new(bool), // pointer to false
	}
	kcl := &KubeClient{dynamicCli: newPatchClient(sc)}
	target := ResourceTarget{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Resource: "storageclasses", Name: "standard"}
	ctx := context.Background()

	require.NoError(t, kcl.ApplyManagedField(ctx, target, nil,
		vals(t, map[string]any{"allowVolumeExpansion": true}), exactOwns("allowVolumeExpansion"), false, false))

	got, _, err := unstructured.NestedBool(getObj(t, kcl, target).Object, "allowVolumeExpansion")
	require.NoError(t, err)
	assert.True(t, got, "scalar field set")

	require.NoError(t, kcl.RestoreManagedField(ctx, target, nil, exactOwns("allowVolumeExpansion")))
	got, _, err = unstructured.NestedBool(getObj(t, kcl, target).Object, "allowVolumeExpansion")
	require.NoError(t, err)
	assert.False(t, got, "scalar field restored to prior value")
}

func TestApplyManagedField_MissingWithoutCreateErrors(t *testing.T) {
	kcl := &KubeClient{dynamicCli: newPatchClient()}
	err := kcl.ApplyManagedField(context.Background(), nsTarget("ghost"), labelsPath,
		vals(t, map[string]any{psaPrefix + "enforce": "baseline"}), psaOwns, true, false)
	require.ErrorContains(t, err, "not found")
}

func TestRestoreManagedField_NoAnnotationNoOp(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"team": "payments"}}}
	kcl := &KubeClient{dynamicCli: newPatchClient(ns)}
	require.NoError(t, kcl.RestoreManagedField(context.Background(), nsTarget("team-a"), labelsPath, psaOwns))
	assert.Equal(t, "payments", getObj(t, kcl, nsTarget("team-a")).GetLabels()["team"])
}

func mustQuantity(s string) resource.Quantity { return resource.MustParse(s) }
