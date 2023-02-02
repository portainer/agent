package yaml

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/rs/zerolog/log"
	libYaml "gopkg.in/yaml.v3"
	v1 "k8s.io/api/apps/v1"
	v1Types "k8s.io/api/core/v1"
	v1AMacTypes "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

type yaml struct {
	fileContent         string
	registryCredentials []agent.RegistryCredentials
}

type Compose struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

type Service struct {
	Image       string   `yaml:"image"`
	Command     []string `yaml:"command"`
	Environment []string `yaml:"environment"`
	Volumes     []string `yaml:"volumes"`
}

func NewYAML(fileContent string, credentials []agent.RegistryCredentials) *yaml {
	return &yaml{
		fileContent:         fileContent,
		registryCredentials: credentials,
	}
}

func (y *yaml) getRegistryCredentialsByImageURL(imageURL string) []agent.RegistryCredentials {
	credentials := []agent.RegistryCredentials{}
	for _, r := range y.registryCredentials {
		domain, err := getRegistryDomain(imageURL)
		if err != nil {
			return nil
		}
		if strings.Contains(r.ServerURL, domain) {
			credentials = append(credentials, r)
		}
	}
	return credentials
}

func (y *yaml) generateImagePullSecrets(namespace string, secretName string, cred agent.RegistryCredentials) v1Types.Secret {
	credentials := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cred.Username, cred.Secret)))
	registryURL := cred.ServerURL
	if !strings.HasPrefix(cred.ServerURL, "http") {
		registryURL = "https://" + registryURL
	}
	secret := v1Types.Secret{
		ObjectMeta: v1AMacTypes.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			".dockerconfigjson": []byte(fmt.Sprintf(`{
				"auths": {
					"%s": {
						"auth": "%s"
					}
				}
			}`, registryURL, credentials)),
		},
		Type: v1Types.SecretTypeDockerConfigJson,
	}
	secret.Kind = "Secret"
	secret.APIVersion = "v1"
	return secret
}

// getRegistryDomain returns the registry domain of the container image reference
// if an image does not contain a registry url, it will be default to docker.io
func getRegistryDomain(image string) (string, error) {
	ref, err := reference.ParseDockerRef(image)
	if err != nil {
		return "", fmt.Errorf("error parsing image (%s): %w", image, err)
	}

	return reference.Domain(ref), nil
}

func (y *yaml) AddImagePullSecrets() (string, error) {
	ymlFiles := strings.Split(y.fileContent, "---\n")
	log.Info().Int("length", len(ymlFiles)).Msg("yaml")

	pullSecrets := make([]v1Types.Secret, 0)
	for i, f := range ymlFiles {
		decode := scheme.Codecs.UniversalDeserializer().Decode

		obj, _, err := decode([]byte(f), nil, nil) // TODO: validate second param
		if err != nil {
			return "", errors.Wrap(err, "Error while decoding original YAML")
		}

		switch o := obj.(type) {
		case *v1.Deployment:
			yml := obj.(*v1.Deployment)
			spec := yml.Spec.Template.Spec
			namespace := yml.GetNamespace()

			for _, c := range spec.Containers {
				creds := y.getRegistryCredentialsByImageURL(c.Image)
				if len(creds) == 0 {
					continue
				}
				for _, cred := range creds {
					imagePullSecretName := slug(cred.ServerURL + cred.Username)
					sec := v1Types.LocalObjectReference{
						Name: imagePullSecretName,
					}
					spec.ImagePullSecrets = append(spec.ImagePullSecrets, sec)

					pullSecret := y.generateImagePullSecrets(namespace, imagePullSecretName, cred)

					pullSecrets = append(pullSecrets, pullSecret)
				}
			}
			yml.Spec.Template.Spec = spec

			ymlStr, err := encodeYAML(yml)
			if err != nil {
				log.Error().Msg("error while encoding YAML with imagePullSecrets")

				continue
			}
			ymlFiles[i] = ymlStr
		default:
			log.Info().Str("type", fmt.Sprintf("%T", obj)).Msg("default case")
			_ = o
		}
	}

	// All pullSecrets to original YAML file
	for _, yml := range pullSecrets {
		y := yml.DeepCopyObject()

		ymlStr, err := encodeYAML(y)
		if err != nil {
			log.Error().Msg("error while encoding YAML with imagePullSecrets")

			continue
		}

		ymlFiles = append(ymlFiles, ymlStr)
	}

	return strings.Join(ymlFiles, "---\n"), nil
}

func (y *yaml) AddCredentialsAsEnvForSpecificService(serviceName string) (string, error) {
	envs := make(map[string]string)

	for _, cred := range y.registryCredentials {
		envs["REGISTRY_USED"] = "1"
		envs["REGISTRY_USERNAME"] = cred.Username
		envs["REGISTRY_PASSWORD"] = cred.Secret
		break
	}
	return addEnvsForSpecificService(y.fileContent, serviceName, envs)
}

// Utility methods
var re = regexp.MustCompile("[^a-z0-9]+")

func slug(s string) string {
	return strings.Trim(re.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func encodeYAML(yml runtime.Object) (string, error) {
	var buf bytes.Buffer

	e := json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil)
	err := e.Encode(yml, &buf)

	return buf.String(), err
}

func addEnvsForSpecificService(fileContent, serviceName string, envs map[string]string) (string, error) {
	var compose Compose
	err := libYaml.Unmarshal([]byte(fileContent), &compose)
	if err != nil {
		return "", errors.Wrap(err, "Error while unmarshalling the docker compose file content")
	}

	if !validateComposeFile(&compose, serviceName) {
		return "", errors.New("Fail to validate the compose file content")
	}

	service, ok := compose.Services[serviceName]
	if !ok {
		return "", errors.Wrap(err, fmt.Sprintf("Cannot find the service: %s", serviceName))
	}

	for k, v := range envs {
		service.Environment = append(service.Environment, fmt.Sprintf("%s=%s", k, v))
	}

	// Needs to reorder the elements in the env array, as Golang built-in array do not guarantee
	// any specific order of their elements
	sort.Strings(service.Environment)
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
