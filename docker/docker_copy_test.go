package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/stretchr/testify/require"
)

func init() {
	fips.InitFIPS(false)
}

func TestBuildRemoveDirCmd(t *testing.T) {
	testCases := []struct {
		name        string
		src         string
		dst         string
		fips        bool
		expectedCmd []string
	}{
		{
			name:        "simple case",
			src:         "/src/test/dir1",
			dst:         "/dest/test",
			fips:        false,
			expectedCmd: []string{"remove-dir", "/dest/test/dir1"},
		},
		{
			name:        "empty src",
			src:         "",
			dst:         "/dest/test",
			fips:        false,
			expectedCmd: []string{"remove-dir", "/dest/test"},
		},
		{
			name:        "empty dest",
			src:         "/src/test/dir1",
			dst:         "",
			fips:        false,
			expectedCmd: []string{"remove-dir", "dir1"},
		},
		{
			name:        "fips",
			src:         "/src/test/dir1",
			dst:         "/dest/test",
			fips:        true,
			expectedCmd: []string{"--fips-mode", "remove-dir", "/dest/test/dir1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd := buildRemoveDirCmd(tc.src, tc.dst, tc.fips)
			require.Equal(t, tc.expectedCmd, gotCmd)
		})
	}
}

func TestCreateContainerConfig(t *testing.T) {
	testCases := []struct {
		name           string
		cmd            []string
		fips           bool
		expectedConfig *container.Config
	}{
		{
			name: "non-fips",
			cmd:  []string{"remove-dir", "test-dir"},
			fips: false,
			expectedConfig: &container.Config{
				Cmd:   []string{"remove-dir", "test-dir"},
				Image: "portainer/compose-unpacker:" + portainer.APIVersion,
			},
		},
		{
			name: "fips",
			cmd:  []string{"remove-dir", "test-dir"},
			fips: true,
			expectedConfig: &container.Config{
				Cmd:   []string{"remove-dir", "test-dir"},
				Image: "portainer/compose-unpacker:" + portainer.APIVersion,
				Env:   []string{"GODEBUG=fips140=on"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := createContainerConfig(tc.cmd, tc.fips)
			require.Equal(t, tc.expectedConfig, config)
		})
	}
}
