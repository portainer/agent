package docker

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	agent "github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/stretchr/testify/assert"
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

func TestGetUnpackerImage(t *testing.T) {
	testCases := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "default image when env var not set",
			envValue: "",
			expected: agent.DefaultUnpackerImage,
		},
		{
			name:     "custom image from env var",
			envValue: "my-registry/my-unpacker:latest",
			expected: "my-registry/my-unpacker:latest",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(agent.ComposeUnpackerImageEnvVar, tc.envValue)
			require.Equal(t, tc.expected, getUnpackerImage())
		})
	}
}

type fakeDockerEndpoint struct {
	status int
	body   string
}

func startFakeDockerServer(t *testing.T, overrides map[string]fakeDockerEndpoint) *httptest.Server {
	t.Helper()

	responses := map[string]fakeDockerEndpoint{
		"images/create":     {http.StatusOK, ""},
		"containers/create": {http.StatusCreated, `{"Id":"fake-container-id","Warnings":[]}`},
		"/start":            {http.StatusNoContent, ""},
		"/wait":             {http.StatusOK, `{"StatusCode":0}`},
		"/archive":          {http.StatusOK, ""},
		"DELETE":            {http.StatusNoContent, ""},
	}
	maps.Copy(responses, overrides)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cfg fakeDockerEndpoint
		var found bool

		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/images/create"):
			cfg, found = responses["images/create"], true
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			cfg, found = responses["containers/create"], true
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			cfg, found = responses["/start"], true
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			cfg, found = responses["/wait"], true
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/archive"):
			cfg, found = responses["/archive"], true
		case r.Method == http.MethodDelete:
			cfg, found = responses["DELETE"], true
		}

		if !found {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(cfg.status)
		if cfg.body != "" {
			_, err := w.Write([]byte(cfg.body))
			assert.NoError(t, err)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func TestRemoveAndCopy(t *testing.T) {
	testCases := []struct {
		name        string
		overrides   map[string]fakeDockerEndpoint
		needCopy    bool
		expectError bool
	}{
		{
			name: "pull image fails",
			overrides: map[string]fakeDockerEndpoint{
				"images/create": {http.StatusInternalServerError, `{"message":"pull failed"}`},
			},
			expectError: true,
		},
		{
			name: "create container fails",
			overrides: map[string]fakeDockerEndpoint{
				"containers/create": {http.StatusInternalServerError, `{"message":"create failed"}`},
			},
			expectError: true,
		},
		{
			name: "start container fails",
			overrides: map[string]fakeDockerEndpoint{
				"/start": {http.StatusInternalServerError, `{"message":"start failed"}`},
			},
			expectError: true,
		},
		{
			name: "wait returns error",
			overrides: map[string]fakeDockerEndpoint{
				"/wait": {http.StatusInternalServerError, `{"message":"wait failed"}`},
			},
			expectError: true,
		},
		{
			name:     "remove only succeeds",
			needCopy: false,
		},
		{
			name:     "copy succeeds",
			needCopy: true,
		},
		{
			name: "copy fails",
			overrides: map[string]fakeDockerEndpoint{
				"/archive": {http.StatusInternalServerError, `{"message":"copy failed"}`},
			},
			needCopy:    true,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			server := startFakeDockerServer(t, tc.overrides)
			t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())

			err := removeAndCopy(src, "/dst", 1, "test-stack", tc.needCopy)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
