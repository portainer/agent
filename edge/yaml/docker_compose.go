package yaml

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/aws"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type DockerComposeYaml struct {
	FileContent         string
	RegistryCredentials []agent.RegistryCredentials
	awsConfig           *agent.AWSConfig
}

type Compose struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

type Service struct {
	Image       string   `yaml:"image"`
	Labels      []string `yaml:"labels,omitempty"`
	Command     []string `yaml:"command,omitempty"`
	Environment []string `yaml:"environment,omitempty"`
	Volumes     []string `yaml:"volumes,omitempty"`
}

func NewDockerComposeYAML(fileContent string, credentials []agent.RegistryCredentials, config *agent.AWSConfig) *DockerComposeYaml {
	return &DockerComposeYaml{
		FileContent:         fileContent,
		RegistryCredentials: credentials,
		awsConfig:           config,
	}
}

func (y *DockerComposeYaml) AddCredentialsAsEnvForSpecificService(serviceName string) (string, error) {
	var compose Compose

	// Parse file content to the object with yaml struct
	err := yaml.Unmarshal([]byte(y.FileContent), &compose)
	if err != nil {
		return "", errors.Wrap(err, "Error while unmarshalling the docker compose file content")
	}

	if !validateComposeFile(&compose, serviceName) {
		return "", errors.New("Failed to validate the compose file content")
	}

	// Extract registry server url from compose object
	service, ok := compose.Services[serviceName]
	if !ok {
		return "", fmt.Errorf("failed to find the service: %s", serviceName)
	}

	serverUrl, err := extractRegistryServerUrl(service.Image)
	if err != nil {
		return "", err
	}

	// Generate envs
	envs := make(map[string]string)
	if y.awsConfig != nil {
		log.Info().Msg("using local AWS config for credential lookup")

		// Exchange ECR credential with ECR certificate
		c, err := aws.DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(serverUrl, y.awsConfig)
		if err != nil {
			return "", err
		}

		envs["REGISTRY_USED"] = "1"
		envs["REGISTRY_ECR_CERTIFICATE_ENABLED"] = "1"
		envs["REGISTRY_USERNAME"] = c.Username
		envs["REGISTRY_PASSWORD"] = c.Secret

	} else if len(y.RegistryCredentials) > 0 {
		log.Info().Msg("using private registry credential")

		for _, cred := range y.RegistryCredentials {
			if serverUrl == cred.ServerURL {
				log.Info().Str("registry server url", cred.ServerURL)

				envs["REGISTRY_USED"] = "1"
				envs["REGISTRY_USERNAME"] = cred.Username
				envs["REGISTRY_PASSWORD"] = cred.Secret
				break
			}
		}
	}

	return updateServiceWithEnv(compose, serviceName, envs)
}

func updateServiceWithEnv(compose Compose, serviceName string, envs map[string]string) (string, error) {
	log.Info().Int("number", len(envs)).Msg("environment variable")
	service, ok := compose.Services[serviceName]
	if !ok {
		return "", fmt.Errorf("failed to find the service: %s", serviceName)
	}

	if service.Environment == nil {
		service.Environment = make([]string, 0)
	}

	for k, v := range envs {
		service.Environment = append(service.Environment, fmt.Sprintf("%s=%s", k, v))
	}

	compose.Services[serviceName] = service

	// Marshal the Compose object into a byte slice.
	yamlBytes, err := yaml.Marshal(compose)
	if err != nil {
		log.Error().Msg("failed to encode compose to yaml file")
		return "", errors.Wrap(err, "failed to encode compose to yaml file")
	}
	return string(yamlBytes), nil
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

func extractRegistryServerUrl(imageName string) (string, error) {
	if imageName == "" {
		return "", errors.New("No image name provided")
	}
	parts := strings.Split(imageName, "/")
	if len(parts) >= 2 && strings.Contains(parts[0], ".") {
		return parts[0], nil
	}
	return "", nil
}
