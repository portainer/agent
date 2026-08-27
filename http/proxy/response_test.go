package proxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/portainer/agent"

	"github.com/stretchr/testify/require"
)

func newResponse(body string) *http.Response {
	return &http.Response{Body: io.NopCloser(strings.NewReader(body))}
}

// reproduceDockerAPIResponse and responseToJSONArray branch on the request path
// prefix. They rely on the caller passing an already version-trimmed path
// (cluster.go trims via httprequest.TrimDockerVersion); these tests document
// that contract, including that a still-versioned /v1.47/volumes path would NOT
// be recognised as a volume list.
func TestReproduceDockerAPIResponse(t *testing.T) {
	data := []any{
		map[string]any{"Id": "a"},
		map[string]any{"Id": "b"},
	}

	t.Run("network list is returned as an array", func(t *testing.T) {
		require.Equal(t, data, reproduceDockerAPIResponse(data, "/networks"))
	})

	t.Run("volume list is wrapped in a Volumes object", func(t *testing.T) {
		result, ok := reproduceDockerAPIResponse(data, "/volumes").(map[string]any)
		require.True(t, ok)
		require.Equal(t, data, result["Volumes"])
	})

	t.Run("versioned volume path is not recognised without trimming", func(t *testing.T) {
		_, ok := reproduceDockerAPIResponse(data, "/v1.47/volumes").(map[string]any)
		require.False(t, ok, "an untrimmed path must not match the /volumes special case")
	})
}

func TestResponseToJSONArray(t *testing.T) {
	t.Run("network list body is decoded as an array", func(t *testing.T) {
		response := newResponse(`[{"Id":"a"},{"Id":"b"}]`)
		t.Cleanup(func() {
			err := response.Body.Close()
			require.NoError(t, err)
		})
		data, err := responseToJSONArray(response, "/networks")
		require.NoError(t, err)
		require.Len(t, data, 2)
	})

	t.Run("volume list is extracted from the Volumes property", func(t *testing.T) {
		response := newResponse(`{"Volumes":[{"Name":"v1"}]}`)
		t.Cleanup(func() {
			err := response.Body.Close()
			require.NoError(t, err)
		})
		data, err := responseToJSONArray(response, "/volumes")
		require.NoError(t, err)
		require.Len(t, data, 1)
	})

	t.Run("null Volumes property yields an empty slice", func(t *testing.T) {
		response := newResponse(`{"Volumes":null}`)
		t.Cleanup(func() {
			err := response.Body.Close()
			require.NoError(t, err)
		})
		data, err := responseToJSONArray(response, "/volumes")
		require.NoError(t, err)
		require.Empty(t, data)
	})

	t.Run("docker error message is surfaced as an error", func(t *testing.T) {
		response := newResponse(`{"message":"boom"}`)
		t.Cleanup(func() {
			err := response.Body.Close()
			require.NoError(t, err)
		})
		_, err := responseToJSONArray(response, "/networks")
		require.EqualError(t, err, "boom")
	})
}

// decorateObject is what stamps each aggregated resource with the node it came
// from. The absence of this decoration (when the request skipped aggregation)
// is exactly what caused the "cannot read properties of undefined (NodeRole)"
// crash on the frontend.
func TestDecorateObject(t *testing.T) {
	result := decorateObject(map[string]any{"Id": "net1"}, "node1")

	object, ok := result.(map[string]any)
	require.True(t, ok)

	metadata, ok := object[agent.ResponseMetadataKey].(agent.Metadata)
	require.True(t, ok)
	require.Equal(t, "node1", metadata.Agent.NodeName)
}
