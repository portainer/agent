package yaml

import (
	"strings"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestAddCredentialsAsEnvForSpecificService(t *testing.T) {
	composeFileContent := `
version: "3"
services:
  updater:
    image: registry.example.com/portainer/updater:latest
`
	registryCredentials := []edge.RegistryCredentials{
		{
			ServerURL: "registry.example.com",
			Username:  "user123",
			Secret:    "pass123",
		},
	}

	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, registryCredentials, nil)
	envVars, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")

	require.NoError(t, err)

	expectedEnv := map[string]string{
		"REGISTRY_USERNAME": "user123",
		"REGISTRY_PASSWORD": "pass123",
	}
	assert.Len(t, envVars, len(expectedEnv))
	for _, pair := range envVars {
		expectedVal, exists := expectedEnv[pair.Name]
		assert.True(t, exists, "unexpected env var: %s", pair.Name)
		assert.Equal(t, expectedVal, pair.Value, "unexpected value for env var %s", pair.Name)
	}

	var compose Compose
	require.NoError(t, yaml.Unmarshal([]byte(updatedYAML), &compose))

	updater, ok := compose.Services["updater"]
	require.True(t, ok, "updater service not found in updated YAML")
	assert.Contains(t, updater.Environment, "REGISTRY_USED=1")
	assert.Contains(t, updater.Environment, "REGISTRY_USERNAME=${REGISTRY_USERNAME}")
	assert.Contains(t, updater.Environment, "REGISTRY_PASSWORD=${REGISTRY_PASSWORD}")
}

func TestUpdateServiceWithEnv(t *testing.T) {
	compose := Compose{
		Services: map[string]Service{
			"updater": {
				Image: "portainer/portainer-updater:latest",
				Labels: []string{
					"io.portainer.hideStack=true",
					"io.portainer.updater=true",
				},
				Command: []string{
					"portainer", "--image", "portainerci/portainer:2.18", "--env-type", "standalone",
				},
				Volumes: []string{
					"/var/run/docker.sock:/var/run/docker.sock",
				},
			},
		},
	}
	serviceName := "updater"
	envs := map[string]string{
		"ENV_VAR_1": "value1",
		"ENV_VAR_2": "value2",
	}

	updatedYAML, err := updateServiceWithEnv(compose, serviceName, envs)
	if err != nil {
		t.Errorf("error while updating service with environment variables: %s", err)
	}

	// Verify that the YAML contains the added environment variables
	if !strings.Contains(updatedYAML, "ENV_VAR_1=value1") || !strings.Contains(updatedYAML, "ENV_VAR_2=value2") {
		t.Errorf("expected environment variables not found in the updated YAML: %s", updatedYAML)
	}
}

func TestExtractRegistryServerUrl(t *testing.T) {
	tests := []struct {
		name      string
		imageName string
		expected  string
		err       error
	}{
		{
			name:      "custom registry",
			imageName: "registry.example.com/namespace/my-image:latest",
			expected:  "registry.example.com",
			err:       nil,
		},
		{
			name:      "custom registry without namespace",
			imageName: "registry.example.com/my-image:latest",
			expected:  "registry.example.com",
			err:       nil,
		},
		{
			name:      "custom registry with port number",
			imageName: "registry.example.com:5000/namespace/my-image:latest",
			expected:  "registry.example.com:5000",
			err:       nil,
		},
		{
			name:      "custom registry with scheme",
			imageName: "http://registry.example.com:5000/namespace/my-image:latest",
			expected:  "http://registry.example.com:5000",
			err:       nil,
		},
		{
			name:      "custom registry with scheme, but namespace",
			imageName: "http://registry.example.com:5000/my-image:latest",
			expected:  "http://registry.example.com:5000",
			err:       nil,
		},
		{
			name:      "namespace + image",
			imageName: "namespace/my-image:latest",
			expected:  "",
			err:       nil,
		},
		{
			name:      "image name only",
			imageName: "ubuntu:latest",
			expected:  "",
			err:       nil,
		},
		{
			name:      "empty image name",
			imageName: "",
			expected:  "",
			err:       errors.New("No image name provided"),
		},
		{
			name:      "invalid image name",
			imageName: "my-image:latest",
			expected:  "",
			err:       errors.New("invalid image name"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := extractRegistryServerUrl(tt.imageName)

			if err != nil && tt.err == nil || err != nil && err.Error() != tt.err.Error() {
				t.Errorf("Test case %s failed: expected error %v, but got error %v", tt.name, tt.err, err)
			}
			if actual != tt.expected {
				t.Errorf("Test case %s failed: expected %v, but got %v", tt.name, tt.expected, actual)
			}
		})
	}
}

func TestAddCredentialsAsEnvForSpecificService_InvalidYAML(t *testing.T) {
	composeFileContent := `::: definitely not valid yaml :::`
	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, nil, nil)

	envVars, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")

	assertBasicErrorResult(t, envVars, updatedYAML, err)
	assert.Contains(t, err.Error(), "unmarshalling")
}

func TestAddCredentialsAsEnvForSpecificService_PreservesUnknownFields(t *testing.T) {
	t.Parallel()
	// The obsolete version key must not be required, and fields not modeled by the Service and Compose structs must survive the rewrite
	composeFileContent := `
services:
  updater:
    image: registry.example.com/portainer/updater:latest
    network_mode: none
    deploy:
      mode: replicated
      replicas: 1
networks:
  custom: {}
`
	registryCredentials := []edge.RegistryCredentials{
		{
			ServerURL: "registry.example.com",
			Username:  "user123",
			Secret:    "pass123",
		},
	}

	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, registryCredentials, nil)

	_, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")
	require.NoError(t, err)

	var compose map[string]any
	err = yaml.Unmarshal([]byte(updatedYAML), &compose)
	require.NoError(t, err)
	require.NotContains(t, compose, "version")
	require.Contains(t, compose, "networks")

	services, ok := compose["services"].(map[string]any)
	require.True(t, ok)

	updater, ok := services["updater"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "none", updater["network_mode"])
	require.Equal(t, map[string]any{"mode": "replicated", "replicas": 1}, updater["deploy"])
	require.Contains(t, updater["environment"], "REGISTRY_USED=1")
}

func TestAddCredentialsAsEnvForSpecificService_NoServices(t *testing.T) {
	// Version present, but no services key
	composeFileContent := `
version: "3"
`
	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, nil, nil)

	envVars, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")

	assertBasicErrorResult(t, envVars, updatedYAML, err)
	assert.Equal(t, "Failed to validate the compose file content", err.Error())
}

func TestAddCredentialsAsEnvForSpecificService_ServiceNotFound(t *testing.T) {
	// Service name requested does not exist
	composeFileContent := `
version: "3"
services:
  somethingelse:
    image: registry.example.com/portainer/updater:latest
`
	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, nil, nil)

	envVars, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")

	assertBasicErrorResult(t, envVars, updatedYAML, err)
	assert.Equal(t, "Failed to validate the compose file content", err.Error(), "validation should fail because service is missing")
}

func TestAddCredentialsAsEnvForSpecificService_EmptyImageName(t *testing.T) {
	// Service present but image is empty -> extractRegistryServerUrl should error
	composeFileContent := `
version: "3"
services:
  updater:
    image: ""
`
	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, nil, nil)

	envVars, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")

	assertBasicErrorResult(t, envVars, updatedYAML, err)
	assert.Equal(t, "No image name provided", err.Error())
}

func TestAddCredentialsAsEnvForSpecificService_NoMatchingCredentials(t *testing.T) {
	// Credentials provided but registry does not match service image -> should not error, just no env vars injected
	composeFileContent := `
version: "3"
services:
  updater:
    image: other.registry.example.com/portainer/updater:latest
`
	registryCredentials := []edge.RegistryCredentials{
		{
			ServerURL: "registry.example.com",
			Username:  "user123",
			Secret:    "pass123",
		},
	}

	dockerComposeYAML := NewDockerComposeYAML(composeFileContent, registryCredentials, nil)
	envVars, updatedYAML, err := dockerComposeYAML.AddCredentialsAsEnvForSpecificService("updater")
	require.NoError(t, err)

	// No env vars returned (no match)
	require.Empty(t, envVars)

	// Updated YAML should not contain placeholder environment variables
	assert.NotContains(t, updatedYAML, "REGISTRY_USED=1")
	assert.NotContains(t, updatedYAML, "REGISTRY_USERNAME=${REGISTRY_USERNAME}")
	assert.NotContains(t, updatedYAML, "REGISTRY_PASSWORD=${REGISTRY_PASSWORD}")
}

func TestUpdateServiceWithEnv_ServiceNotFound(t *testing.T) {
	compose := Compose{
		Services: map[string]Service{}, // empty map
	}

	updatedYAML, err := updateServiceWithEnv(compose, "updater", map[string]string{"X": "Y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find the service: updater")
	assert.Empty(t, updatedYAML)
}

// Helper to ensure common assertions on error cases.
func assertBasicErrorResult(t *testing.T, envVars []portainer.Pair, updatedYAML string, err error) {
	t.Helper()
	require.Error(t, err)
	assert.Empty(t, updatedYAML, "updatedYAML should be empty on error")
	assert.Nil(t, envVars, "envVars should be nil on error")
}
