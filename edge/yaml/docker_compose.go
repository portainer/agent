package yaml

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/rs/zerolog/log"
	libYaml "gopkg.in/yaml.v3"
)

type DockerComposeYaml struct {
	yaml
}

type Compose struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

type Service struct {
	Image       string   `yaml:"image"`
	Labels      []string `yaml:"labels"`
	Command     []string `yaml:"command"`
	Environment []string `yaml:"environment"`
	Volumes     []string `yaml:"volumes"`
}

func NewDockerComposeYAML(fileContent string, credentials []agent.RegistryCredentials) *DockerComposeYaml {
	return &DockerComposeYaml{
		yaml: NewYAML(fileContent, credentials),
	}
}

func (y *DockerComposeYaml) AddCredentialsAsEnvForSpecificService(serviceName string) (string, error) {
	envs := make(map[string]string)

	log.Info().Int("registry credential number", len(y.registryCredentials)).Msg("private rigstry")
	for _, cred := range y.registryCredentials {
		envs["REGISTRY_USED"] = "1"
		envs["REGISTRY_USERNAME"] = cred.Username
		envs["REGISTRY_PASSWORD"] = cred.Secret
		break
	}
	return addEnvsForSpecificService(y.fileContent, serviceName, envs)
}

func addEnvsForSpecificService(fileContent, serviceName string, envs map[string]string) (string, error) {
	var compose Compose
	err := libYaml.Unmarshal([]byte(fileContent), &compose)
	if err != nil {
		return "", errors.Wrap(err, "Error while unmarshalling the docker compose file content")
	}

	if !validateComposeFile(&compose, serviceName) {
		return "", errors.New("Failed to validate the compose file content")
	}

	service, ok := compose.Services[serviceName]
	if !ok {
		return "", errors.Wrap(err, fmt.Sprintf("Cannot find the service: %s", serviceName))
	}

	log.Info().Int("number", len(envs)).Msg("environment variable")
	if service.Environment == nil {
		service.Environment = make([]string, 0)
	}
	for k, v := range envs {
		service.Environment = append(service.Environment, fmt.Sprintf("%s=%s", k, v))
	}

	compose.Services[serviceName] = service

	var b bytes.Buffer
	encoder := libYaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(compose); err != nil {
		log.Error().Msg("error while encoding YAML with adding environment variables")
		return "", errors.Wrap(err, "Error while encoding YAML with adding environment variables")
	}

	return b.String(), nil
}

func validateComposeFile(compose *Compose, serviceName string) bool {
	if compose == nil {
		return false
	}

	if compose.Version == "" {
		return false
	}

	if len(compose.Services) == 0 {
		return false
	}

	_, ok := compose.Services[serviceName]
	if !ok {
		return false
	}
	return true
}
