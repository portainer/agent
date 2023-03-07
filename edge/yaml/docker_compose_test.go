package yaml

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddEnvironmentVariablesToService(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name: "environment stanza does not exist",
			input: `version: "3"
services:
  updater:
    image: portainer/portainer-updater:latest
    labels:
      - io.portainer.hideStack=true
      - io.portainer.updater=true
    command: ["portainer", "--image", "portainerci/portainer:2.18", "--env-type", "standalone"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock`,
			expected: `version: "3"
services:
  updater:
    image: portainer/portainer-updater:latest
    labels:
      - io.portainer.hideStack=true
      - io.portainer.updater=true
    command:
      - portainer
      - --image
      - portainerci/portainer:2.18
      - --env-type
      - standalone
    environment:
      - TEST_VAR_1=test_value_1
      - TEST_VAR_2=test_value_2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`,
			wantErr: false,
		},
		{
			name: "environment stanza exists",
			input: `version: "3"
services:
  updater:
    image: portainer/portainer-updater:latest
    labels:
      - io.portainer.hideStack=true
      - io.portainer.updater=true
    command: ["portainer", "--image", "portainerci/portainer:2.18", "--env-type", "standalone"]
    environment:
      - SKIP_PULL=1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock`,
			expected: `version: "3"
services:
  updater:
    image: portainer/portainer-updater:latest
    labels:
      - io.portainer.hideStack=true
      - io.portainer.updater=true
    command:
      - portainer
      - --image
      - portainerci/portainer:2.18
      - --env-type
      - standalone
    environment:
      - SKIP_PULL=1
      - TEST_VAR_1=test_value_1
      - TEST_VAR_2=test_value_2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`,
			wantErr: false,
		},
		{
			name: "valid docker compose file content",
			input: `version: "3"
services:
invalid_service:`,
			expected: ``,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envs := map[string]string{
				"TEST_VAR_1": "test_value_1",
				"TEST_VAR_2": "test_value_2",
			}

			result, err := addEnvsForSpecificService(tt.input, "updater", envs)
			assert.Equalf(t, tt.wantErr, err != nil, "received %+v", err)

			if !tt.wantErr {
				assert.Equalf(t, tt.expected, result, "expected result: %s\nactual result: %s", tt.expected, result)
			}
		})
	}
}
