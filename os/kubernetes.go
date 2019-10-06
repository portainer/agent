package os

import (
	"os"

	"github.com/portainer/agent"
)

const (
	KubernetesServiceHost = "KUBERNETES_SERVICE_HOST"
)

func DetermineContainerPlatform() agent.ContainerPlatform {
	serviceHostEnvVar := os.Getenv(KubernetesServiceHost)
	if serviceHostEnvVar != "" {
		return agent.PlatformKubernetes
	}
	return agent.PlatformDocker
}
