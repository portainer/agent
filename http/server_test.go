package http

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge"
	"github.com/stretchr/testify/require"
)

func TestEdgeHandlerSkipsActivityResetForMetricsPrefix(t *testing.T) {
	t.Parallel()
	manager := edge.NewManager(&edge.ManagerParameters{
		Options: &agent.Options{DataPath: t.TempDir()},
	})

	key := base64.RawStdEncoding.EncodeToString([]byte("https://portainer.example|tunnel.example:8000|fingerprint|1"))
	require.NoError(t, manager.SetKey(key))

	server := &APIServer{edgeManager: manager}
	nextCalled := false
	handler := server.edgeHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/subpath", nil)

	handler.ServeHTTP(rec, req)

	require.True(t, nextCalled)
	require.Equal(t, http.StatusNoContent, rec.Code)
}
