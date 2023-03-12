package yaml

import (
	"strings"
	"testing"

	"github.com/pkg/errors"
)

func TestUpdateServiceWithEnv(t *testing.T) {
	compose := Compose{
		Version: "3",
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
			name:      "input without registry server url",
			imageName: "portainer/agent:latest",
			expected:  "",
			err:       nil,
		},
		{
			name:      "input with registry server url",
			imageName: "example.com/portainer/agent:latest",
			expected:  "example.com",
			err:       nil,
		},
		{
			name:      "empty image name",
			imageName: "",
			expected:  "",
			err:       errors.New("No image name provided"),
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
