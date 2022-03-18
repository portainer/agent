package os

import (
	"os"

	"github.com/portainer/agent"
)

const (
	PodmanMode            = "PODMAN"
	KubernetesServiceHost = "KUBERNETES_SERVICE_HOST"
	KubernetesPodIP       = "KUBERNETES_POD_IP"
	NomadJobName          = "NOMAD_JOB_NAME"
)

// DetermineContainerPlatform will check for the existence of the PODMAN_MODE
// or KUBERNETES_SERVICE_HOST environment variable to determine if
// the container is running on Podman or inside the Kubernetes platform.
// Defaults to Docker otherwise.
func DetermineContainerPlatform() agent.ContainerPlatform {
	podmanModeEnvVar := os.Getenv(PodmanMode)
	if podmanModeEnvVar == "1" {
		return agent.PlatformPodman
	}
	serviceHostKubernetesEnvVar := os.Getenv(KubernetesServiceHost)
	if serviceHostKubernetesEnvVar != "" {
		return agent.PlatformKubernetes
	}
	nomadJobName := os.Getenv(NomadJobName)
	if nomadJobName != "" {
		return agent.PlatformNomad
	}

	return agent.PlatformDocker
}

// GetKubernetesPodIP returns the pod IP address through the KUBERNETES_POD_IP environment variable.
// This environment variable must be specified in the Agent deployment specs.
func GetKubernetesPodIP() string {
	return os.Getenv(KubernetesPodIP)
}
