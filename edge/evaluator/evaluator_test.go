package evaluator

import (
	"os"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPreservesExistingTSDBDataDir(t *testing.T) {
	t.Parallel()
	dataDir := filesystem.JoinPaths(t.TempDir(), "tsdb")
	require.NoError(t, os.MkdirAll(dataDir, 0o750))
	markerFile := filesystem.JoinPaths(dataDir, "marker-file")
	require.NoError(t, os.WriteFile(markerFile, []byte("keep"), 0o600))

	svc, err := New(Config{
		DataDir:    dataDir,
		EndpointID: portainer.EndpointID(1),
	})
	require.NoError(t, err)
	t.Cleanup(svc.Stop)

	// The marker file should still exist — ensureDataDir does not wipe.
	_, err = os.Stat(markerFile)
	require.NoError(t, err)

	entries, err := os.ReadDir(dataDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}
