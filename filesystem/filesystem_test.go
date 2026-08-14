package filesystem

import (
	"os"
	"testing"

	"github.com/portainer/agent/constants"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/require"
)

func TestBuildPathToFileInsideVolume(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		volumeID     string
		filePath     string
		expectedPath string
		expectErr    bool
	}{
		{
			name:         "simple file at root",
			volumeID:     "my_volume",
			filePath:     "file.txt",
			expectedPath: "/var/lib/docker/volumes/my_volume/_data/file.txt",
		},
		{
			name:         "empty file path",
			volumeID:     "my_volume",
			filePath:     "",
			expectedPath: "/var/lib/docker/volumes/my_volume/_data",
		},
		{
			name:         "nested file path",
			volumeID:     "my_volume",
			filePath:     "subdir/nested/file.txt",
			expectedPath: "/var/lib/docker/volumes/my_volume/_data/subdir/nested/file.txt",
		},
		{
			name:      "path traversal with ..",
			volumeID:  "my_volume",
			filePath:  "../etc/passwd",
			expectErr: true,
		},
		{
			name:      "path traversal embedded in segments",
			volumeID:  "my_volume",
			filePath:  "subdir/../../etc/passwd",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildPathToFileInsideVolume(tc.volumeID, tc.filePath)

			if tc.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedPath, got)
		})
	}
}

func TestBuildPathToFileInsideVolumeFromMountpoint(t *testing.T) {
	t.Parallel()

	t.Run("direct mountpoint exists (-v /var/lib/docker/volumes:/var/lib/docker/volumes)", func(t *testing.T) {
		t.Parallel()

		// Simulate the volumes-only mount: the mountpoint path exists directly
		// inside the container at the same path as on the host.
		root := t.TempDir()
		mountpoint := filesystem.JoinPaths(root, constants.SystemVolumePath, "test_volume", "_data")
		require.NoError(t, os.MkdirAll(mountpoint, 0o755))

		got, err := BuildPathToFileInsideVolumeFromMountpoint(mountpoint, "myfile.txt")
		require.NoError(t, err)
		require.Equal(t, filesystem.JoinPaths(mountpoint, "myfile.txt"), got)
	})

	t.Run("direct mountpoint exists, empty filePath", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		mountpoint := filesystem.JoinPaths(root, constants.SystemVolumePath, "test_volume", "_data")
		require.NoError(t, os.MkdirAll(mountpoint, 0o755))

		got, err := BuildPathToFileInsideVolumeFromMountpoint(mountpoint, "")
		require.NoError(t, err)
		require.Equal(t, mountpoint, got)
	})

	t.Run("neither direct nor /host mountpoint exists — returns ErrSystemVolumePathNotMounted", func(t *testing.T) {
		t.Parallel()

		_, err := BuildPathToFileInsideVolumeFromMountpoint("/nonexistent/mountpoint/that/cannot/exist", "file.txt")
		require.ErrorIs(t, err, ErrSystemVolumePathNotMounted)
	})

	t.Run("path traversal in filePath — returns error", func(t *testing.T) {
		t.Parallel()

		_, err := BuildPathToFileInsideVolumeFromMountpoint("/some/mountpoint", "../etc/passwd")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSystemVolumePathNotMounted)
	})

	t.Run("FileExists error on direct mountpoint — error propagated", func(t *testing.T) {
		t.Parallel()

		// Use a file as a path component so os.Stat returns a real error (not IsNotExist).
		dir := t.TempDir()
		file := filesystem.JoinPaths(dir, "notadir")
		require.NoError(t, os.WriteFile(file, []byte{}, 0o600))

		_, err := buildPathToFileInsideVolumeFromMountpoint(filesystem.JoinPaths(file, "child"), "file.txt", dir)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSystemVolumePathNotMounted)
	})

	t.Run("FileExists error on host mountpoint — error propagated", func(t *testing.T) {
		t.Parallel()

		// Direct mountpoint does not exist (nonexistent dir), but the host-prefixed
		// path hits a file used as a directory component, triggering a real error.
		dir := t.TempDir()
		file := filesystem.JoinPaths(dir, "notadir")
		require.NoError(t, os.WriteFile(file, []byte{}, 0o600))

		// hostPrefix = file (a regular file), so filesystem.JoinPaths(file, mountpoint) will
		// cause os.Stat to fail with a non-IsNotExist error.
		_, err := buildPathToFileInsideVolumeFromMountpoint("/nonexistent/mountpoint", "file.txt", file)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSystemVolumePathNotMounted)
	})

	t.Run("/host mountpoint exists — correct path returned", func(t *testing.T) {
		t.Parallel()

		// Direct mountpoint does not exist; host-prefixed mountpoint does.
		// Use a mountpoint path rooted inside a temp dir so the direct stat
		// returns "not found" (not a permission error), then place the actual
		// directory under hostPrefix so the /host lookup succeeds.
		base := t.TempDir()
		mountpoint := filesystem.JoinPaths(base, "volumes", "test_volume", "_data")
		hostPrefix := t.TempDir()
		hostMountpoint := filesystem.JoinPaths(hostPrefix, mountpoint)
		require.NoError(t, os.MkdirAll(hostMountpoint, 0o755))

		got, err := buildPathToFileInsideVolumeFromMountpoint(mountpoint, "myfile.txt", hostPrefix)
		require.NoError(t, err)
		require.Equal(t, filesystem.JoinPaths(hostMountpoint, "myfile.txt"), got)
	})
}

func TestFileExists(t *testing.T) {
	t.Parallel()

	t.Run("existing file returns true", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		f := filesystem.JoinPaths(dir, "test.txt")
		require.NoError(t, os.WriteFile(f, []byte("data"), 0o600))

		exists, err := FileExists(f)
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("existing directory returns true", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		exists, err := FileExists(dir)
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("non-existing path returns false", func(t *testing.T) {
		t.Parallel()

		exists, err := FileExists("/this/path/does/not/exist/anywhere")
		require.NoError(t, err)
		require.False(t, exists)
	})
}
