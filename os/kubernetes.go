package os

import (
	"os"

	"github.com/portainer/agent"
)

const (
	KubernetesServiceHost = "KUBERNETES_SERVICE_HOST"
	KubernetesPodIP       = "KUBERNETES_POD_IP"
)

// DetermineContainerPlatform will check for the existence of the
// KUBERNETES_SERVICE_HOST environment variable to determine if
// the container is running inside the Kubernetes platform.
// Defaults to Docker otherwise.
func DetermineContainerPlatform() agent.ContainerPlatform {
	serviceHostEnvVar := os.Getenv(KubernetesServiceHost)
	if serviceHostEnvVar != "" {
		return agent.PlatformKubernetes
	}
	return agent.PlatformDocker
}

// GetKubernetesPodIP returns the pod IP address through the KUBERNETES_POD_IP environment variable.
// This environment variable must be specified in the Agent deployment specs.
func GetKubernetesPodIP() string {
	return os.Getenv(KubernetesPodIP)
}
