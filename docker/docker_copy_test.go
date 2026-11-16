package docker

import (
	"archive/tar"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/registry"
	agent "github.com/portainer/agent"
	"github.com/portainer/portainer/api/edge"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	fips.InitFIPS(false)
}

func TestBuildRemoveDirCmd(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
				Image: agent.DefaultUnpackerImage,
			},
		},
		{
			name: "fips",
			cmd:  []string{"remove-dir", "test-dir"},
			fips: true,
			expectedConfig: &container.Config{
				Cmd:   []string{"remove-dir", "test-dir"},
				Image: agent.DefaultUnpackerImage,
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

func TestUnpackerImageRegistryAuth(t *testing.T) {
	t.Parallel()

	f := func(unpackerImg string, registryCredentials []edge.RegistryCredentials, expectedCred *edge.RegistryCredentials) {
		t.Helper()

		auth := unpackerImageRegistryAuth(unpackerImg, registryCredentials)

		if expectedCred == nil {
			require.Empty(t, auth)
			return
		}

		decoded, err := base64.URLEncoding.DecodeString(auth)
		require.NoError(t, err)

		var authConfig registry.AuthConfig
		err = json.Unmarshal(decoded, &authConfig)
		require.NoError(t, err)
		require.Equal(t, expectedCred.Username, authConfig.Username)
		require.Equal(t, expectedCred.Secret, authConfig.Password)
		require.Equal(t, expectedCred.ServerURL, authConfig.ServerAddress)
	}

	// no registry credentials attached to the stack
	f("myregistry.example.com/compose-unpacker:latest", nil, nil)

	// none of the attached credentials match the unpacker image's registry
	f("myregistry.example.com/compose-unpacker:latest", []edge.RegistryCredentials{
		{ServerURL: "otherregistry.example.com", Username: "user", Secret: "pass"},
	}, nil)

	// a credential's server URL matches the unpacker image
	matching := edge.RegistryCredentials{ServerURL: "myregistry.example.com", Username: "user2", Secret: "pass2"}
	f("myregistry.example.com/compose-unpacker:latest", []edge.RegistryCredentials{
		{ServerURL: "otherregistry.example.com", Username: "user", Secret: "pass"},
		matching,
	}, &matching)

	// the default unpacker image never matches a private registry credential
	f(agent.DefaultUnpackerImage, []edge.RegistryCredentials{
		{ServerURL: "otherregistry.example.com", Username: "user", Secret: "pass"},
	}, nil)
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

func TestCopyGitStackToHostAndRemoveGitStackFromHost(t *testing.T) {
	src := t.TempDir()
	server := startFakeDockerServer(t, nil)
	t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())

	err := CopyGitStackToHost(src, "/dst", 1, "test-stack", nil)
	require.NoError(t, err)

	err = RemoveGitStackFromHost(src, "/dst", 1, "test-stack", nil)
	require.NoError(t, err)
}

func TestRemoveGitStackFromHost(t *testing.T) {
	testCases := []struct {
		name        string
		overrides   map[string]fakeDockerEndpoint
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
			name: "remove succeeds",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			server := startFakeDockerServer(t, tc.overrides)
			t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())

			err := RemoveGitStackFromHost(src, "/dst", 1, "test-stack", nil)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCopyGitStackToHostOverwritesInPlace(t *testing.T) {
	f := func(overrides map[string]fakeDockerEndpoint, expectError bool) {
		t.Helper()

		src := t.TempDir()
		server := startFakeDockerServer(t, overrides)
		t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())

		err := CopyGitStackToHost(src, "/dst", 1, "test-stack", nil)
		if expectError {
			require.Error(t, err)

			return
		}

		require.NoError(t, err)
	}

	// the copy never starts the unpacker container, so nothing deletes the existing folder
	f(map[string]fakeDockerEndpoint{
		"/start": {http.StatusInternalServerError, `{"message":"start must not be called"}`},
		"/wait":  {http.StatusInternalServerError, `{"message":"wait must not be called"}`},
	}, false)

	// pulling the unpacker image fails
	f(map[string]fakeDockerEndpoint{
		"images/create": {http.StatusInternalServerError, `{"message":"pull failed"}`},
	}, true)

	// creating the unpacker container fails
	f(map[string]fakeDockerEndpoint{
		"containers/create": {http.StatusInternalServerError, `{"message":"create failed"}`},
	}, true)

	// copying the files into the container fails
	f(map[string]fakeDockerEndpoint{
		"/archive": {http.StatusInternalServerError, `{"message":"copy failed"}`},
	}, true)
}

func TestCopyGitStackToHostArchiveRequest(t *testing.T) {
	src := t.TempDir()
	err := os.WriteFile(filesystem.JoinPaths(src, "docker-compose.yml"), []byte("services: {}\n"), 0o600)
	require.NoError(t, err)

	archives := make(chan map[string]string, 1)

	fake := startFakeDockerServer(t, nil)
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/archive") {
			assert.Contains(t, r.URL.Path, "/containers/fake-container-id/archive")
			assert.Equal(t, "/dst", r.URL.Query().Get("path"))

			entries := map[string]string{}
			tr := tar.NewReader(r.Body)

			for {
				hdr, err := tr.Next()
				if errors.Is(err, io.EOF) {
					break
				}

				if !assert.NoError(t, err) {
					break
				}

				content, err := io.ReadAll(tr)
				assert.NoError(t, err)
				entries[hdr.Name] = string(content)
			}

			archives <- entries
		}

		fake.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(recorder.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+recorder.Listener.Addr().String())

	err = CopyGitStackToHost(src, "/dst", 1, "test-stack", nil)
	require.NoError(t, err)

	// the stack files are extracted on top of the destination inside the created container
	entries := <-archives
	require.Equal(t, "services: {}\n", entries[filepath.Base(src)+"/docker-compose.yml"])
}
