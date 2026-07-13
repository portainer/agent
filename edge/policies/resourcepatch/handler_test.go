package resourcepatch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/portainer/agent/kubernetes"
)

const psaPrefix = "pod-security.kubernetes.io/"

type applyCall struct {
	target    kubernetes.ResourceTarget
	fieldPath []string
	values    map[string][]byte
	owns      func(string) bool
	exclusive bool
	create    bool
}

type fakePatcher struct {
	applies  []applyCall
	restored []kubernetes.ResourceTarget
	applyErr error
}

func (f *fakePatcher) ApplyManagedField(_ context.Context, t kubernetes.ResourceTarget, fieldPath []string, values map[string][]byte, owns func(string) bool, exclusive, create bool) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applies = append(f.applies, applyCall{target: t, fieldPath: fieldPath, values: values, owns: owns, exclusive: exclusive, create: create})
	return nil
}

func (f *fakePatcher) RestoreManagedField(_ context.Context, t kubernetes.ResourceTarget, _ []string, _ func(string) bool) error {
	f.restored = append(f.restored, t)
	return nil
}

func (f *fakePatcher) applyFor(name string) (applyCall, bool) {
	for _, c := range f.applies {
		if c.target.Name == name {
			return c, true
		}
	}
	return applyCall{}, false
}

func newHandler(patcher fieldPatcher) *Handler {
	return NewHandler(patcher)(1).(*Handler)
}

func configJSON(t *testing.T, cfg Config) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return raw
}

func nsLabelOp(name string, values map[string]json.RawMessage) portainer.ResourcePatchOperation {
	return portainer.ResourcePatchOperation{
		APIVersion:       "v1",
		Kind:             "Namespace",
		Resource:         "namespaces",
		Name:             name,
		FieldPath:        []string{"metadata", "labels"},
		Values:           values,
		OwnedKeyPrefixes: []string{psaPrefix},
		Exclusive:        true,
		CreateIfMissing:  true,
	}
}

func TestApply_MapsOpsToApplyManagedField(t *testing.T) {
	patcher := &fakePatcher{}
	h := newHandler(patcher)

	cfg := Config{Patches: []portainer.ResourcePatchOperation{
		nsLabelOp("team-a", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"restricted"`)}),
	}}
	require.NoError(t, h.Apply(context.Background(), configJSON(t, cfg)))

	c, ok := patcher.applyFor("team-a")
	require.True(t, ok)
	assert.Equal(t, kubernetes.ResourceTarget{APIVersion: "v1", Kind: "Namespace", Resource: "namespaces", Name: "team-a"}, c.target)
	assert.Equal(t, []string{"metadata", "labels"}, c.fieldPath)
	assert.JSONEq(t, `"restricted"`, string(c.values[psaPrefix+"enforce"]))
	assert.True(t, c.exclusive)
	assert.True(t, c.create)
	require.NotNil(t, c.owns)
	assert.True(t, c.owns(psaPrefix+"enforce"))
	assert.False(t, c.owns("team"))
}

func TestApply_RestoresOpsRemovedFromConfig(t *testing.T) {
	patcher := &fakePatcher{}
	h := newHandler(patcher)
	ctx := context.Background()

	require.NoError(t, h.Apply(ctx, configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{
		nsLabelOp("team-a", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"baseline"`)}),
		nsLabelOp("team-b", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"baseline"`)}),
	}})))

	require.NoError(t, h.Apply(ctx, configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{
		nsLabelOp("team-a", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"restricted"`)}),
	}})))

	require.Len(t, patcher.restored, 1)
	assert.Equal(t, "team-b", patcher.restored[0].Name)
}

func TestApply_InvalidConfigReturnsError(t *testing.T) {
	op := func(apiVersion, kind, resource, name string) portainer.ResourcePatchOperation {
		return portainer.ResourcePatchOperation{APIVersion: apiVersion, Kind: kind, Resource: resource, Name: name}
	}

	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "malformed JSON", raw: json.RawMessage(`{`)},
		{name: "missing apiVersion", raw: configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{op("", "Namespace", "namespaces", "team-a")}})},
		{name: "missing kind", raw: configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{op("v1", "", "namespaces", "team-a")}})},
		{name: "missing resource", raw: configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{op("v1", "Namespace", "", "team-a")}})},
		{name: "missing name", raw: configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{op("v1", "Namespace", "namespaces", "")}})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(&fakePatcher{})
			require.Error(t, h.Apply(context.Background(), tt.raw))
		})
	}
}

func TestApply_PropagatesPatcherError(t *testing.T) {
	h := newHandler(&fakePatcher{applyErr: errors.New("boom")})
	err := h.Apply(context.Background(), configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{
		nsLabelOp("team-a", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"baseline"`)}),
	}}))
	require.ErrorContains(t, err, "team-a")
}

func TestRemove_RestoresAllApplied(t *testing.T) {
	patcher := &fakePatcher{}
	h := newHandler(patcher)
	ctx := context.Background()

	require.NoError(t, h.Apply(ctx, configJSON(t, Config{Patches: []portainer.ResourcePatchOperation{
		nsLabelOp("team-a", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"baseline"`)}),
		nsLabelOp("team-b", map[string]json.RawMessage{psaPrefix + "enforce": json.RawMessage(`"baseline"`)}),
	}})))

	require.NoError(t, h.Remove(ctx))
	names := []string{patcher.restored[0].Name, patcher.restored[1].Name}
	assert.ElementsMatch(t, []string{"team-a", "team-b"}, names)

	patcher.restored = nil
	require.NoError(t, h.Remove(ctx))
	assert.Empty(t, patcher.restored)
}
