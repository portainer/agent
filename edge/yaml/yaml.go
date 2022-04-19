package yaml

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"regexp"
	"strings"

	v1Types "k8s.io/api/core/v1"
	v1AMacTypes "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

type yaml struct {
	fileContent string
	registries  map[string]agent.Credentials
}

func NewYAML(fileContent string, registries map[string]agent.Credentials) *yaml {
	return &yaml{
		fileContent: fileContent,
		registries:  registries,
	}
}

func (y *yaml) getRegistryCredentialsByImageURL(imageURL string) []agent.Credentials {
	credentials := []agent.Credentials{}
	for _, r := range y.registries {
		if strings.Contains(imageURL, r.ServerURL) {
			credentials = append(credentials, r)
		}
	}
	return credentials
}

func (y *yaml) getImagePullSecret(namespace string, secretName string, cred agent.Credentials) v1Types.Secret {
	credentials := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cred.Username, cred.Secret)))
	secret := v1Types.Secret{
		ObjectMeta: v1AMacTypes.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			".dockerconfigjson": []byte(fmt.Sprintf(`{
				"auths": {
					"https://index.docker.io/v1/": {
						"auth": "%s"
					}
				}
			}`, credentials)),
		},
		Type: v1Types.SecretTypeDockerConfigJson,
	}
	secret.Kind = "Secret"
	secret.APIVersion = "v1"
	return secret
}

func (y *yaml) AddImagePullSecrets() (string, error) {
	ymlFiles := strings.Split(y.fileContent, "---\n")
	log.Printf("[INFO] yaml length %d", len(ymlFiles))

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

					pullSecret := y.getImagePullSecret(namespace, imagePullSecretName, cred)

					pullSecrets = append(pullSecrets, pullSecret)
				}
			}
			yml.Spec.Template.Spec = spec

			ymlStr, err := encodeYAML(yml)
			if err != nil {
				log.Printf("[ERROR] [edge,stack] error while encoding YAML with imagePullSecrets")
				continue
			}
			ymlFiles[i] = ymlStr
		default:
			fmt.Printf("[INFO] default case %T", obj)
			_ = o
		}
	}

	// All pullSecrets to original YAML file
	for _, yml := range pullSecrets {

		y := yml.DeepCopyObject()
		ymlStr, err := encodeYAML(y)
		if err != nil {
			log.Printf("[ERROR] [edge,stack] error while encoding YAML with imagePullSecrets")
			continue
		}
		ymlFiles = append(ymlFiles, ymlStr)
	}

	return strings.Join(ymlFiles, "---\n"), nil
}

// Utility methods
var re = regexp.MustCompile("[^a-z0-9]+")

func slug(s string) string {
	return strings.Trim(re.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func encodeYAML(yml runtime.Object) (string, error) {
	e := json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil)
	var buf bytes.Buffer
	err := e.Encode(yml, &buf)
	return buf.String(), err
}
