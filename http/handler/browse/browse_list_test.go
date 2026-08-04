package browse

import (
	"testing"

	"github.com/portainer/agent/constants"
	"github.com/portainer/agent/filesystem"
	apifilesystem "github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/require"
)

// stubGetVolumeMountpoint stubs getVolumeMountpointFunc for the duration of
// the test and restores the original via t.Cleanup.
func stubGetVolumeMountpoint(t *testing.T, mountpoint string, err error) {
	t.Helper()
	orig := getVolumeMountpointFunc
	getVolumeMountpointFunc = func(string) (string, error) { return mountpoint, err }
	t.Cleanup(func() { getVolumeMountpointFunc = orig })
}

func TestResolveVolumePath_RootlessDocker_FallsBackToDefaultPath(t *testing.T) {
	// Reproduces C9S-237: Docker reports a Mountpoint rooted at a non-default
	// data root (e.g. rootless Docker's per-user data dir), which is never
	// reachable inside the agent container, and no /host bind mount exists
	// either. The container does, however, have the volumes directory
	// bind-mounted at the legacy default SystemVolumePath, so resolution
	// must fall back to it instead of failing with "Volume path not mounted".
	stubGetVolumeMountpoint(t, "/home/docker/.local/share/docker/volumes/test_volume/_data", nil)

	got, err := resolveVolumePath("test_volume", "file.txt")
	require.NoError(t, err)
	require.Equal(t, apifilesystem.JoinPaths(constants.SystemVolumePath, "test_volume", "_data", "file.txt"), got)
}

func TestResolveVolumePath_MountpointLookupFails_FallsBackToDefaultPath(t *testing.T) {
	stubGetVolumeMountpoint(t, "", assertAnError)

	got, err := resolveVolumePath("test_volume", "file.txt")
	require.NoError(t, err)
	require.Equal(t, apifilesystem.JoinPaths(constants.SystemVolumePath, "test_volume", "_data", "file.txt"), got)
}

func TestResolveVolumePath_MountpointResolves_UsesResolvedPath(t *testing.T) {
	dir := t.TempDir()
	stubGetVolumeMountpoint(t, dir, nil)

	got, err := resolveVolumePath("test_volume", "file.txt")
	require.NoError(t, err)
	require.Equal(t, apifilesystem.JoinPaths(dir, "file.txt"), got)
}

var assertAnError = &browseError{"docker inspect failed"}

func TestResolveVolumePath_PathTraversal_ReturnsError(t *testing.T) {
	stubGetVolumeMountpoint(t, "/some/mountpoint", nil)

	_, err := resolveVolumePath("test_volume", "../etc/passwd")
	require.Error(t, err)
	require.NotErrorIs(t, err, filesystem.ErrSystemVolumePathNotMounted)
}
