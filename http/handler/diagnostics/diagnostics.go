package diagnostics

import (
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/docker"
	"github.com/portainer/agent/kubernetes"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
	"github.com/portainer/portainer/pkg/snapshot"
)

// getDiagnostics returns diagnostic information about the current container platform.
// Podman uses the Docker-compatible diagnostics path.
func (h *Handler) diagnostics(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {
	switch h.containerPlatform {
	case agent.PlatformKubernetes:
		return h.getKubernetesDiagnostics(rw)
	case agent.PlatformDocker, agent.PlatformPodman:
		return h.getDockerDiagnostics(rw)
	default:
		return httperror.InternalServerError(
			"Unsupported container platform. Only Docker, Podman, and Kubernetes are supported.",
			nil,
		)
	}
}

// getKubernetesDiagnostics retrieves diagnostic information for Kubernetes clusters
func (h *Handler) getKubernetesDiagnostics(rw http.ResponseWriter) *httperror.HandlerError {
	cli, err := kubernetes.BuildLocalClient()
	if err != nil {
		return httperror.InternalServerError("Unable to create Kubernetes client for diagnostics", err)
	}

	edgeKey := ""
	if h.edgeManager != nil && h.edgeManager.GetKey() != "" {
		edgeKey = h.edgeManager.GetKey()
	}

	kubernetesSnapshot, err := snapshot.KubernetesSnapshotDiagnostics(cli, edgeKey)
	if err != nil {
		return httperror.InternalServerError("Unable to retrieve Kubernetes diagnostics", err)
	}

	return response.JSON(rw, kubernetesSnapshot)
}

// getDockerDiagnostics retrieves diagnostic information for Docker environments
func (h *Handler) getDockerDiagnostics(rw http.ResponseWriter) *httperror.HandlerError {
	cli, err := docker.NewClient()
	if err != nil {
		return httperror.InternalServerError("Unable to create Docker client for diagnostics", err)
	}

	edgeKey := ""
	if h.edgeManager != nil && h.edgeManager.GetKey() != "" {
		edgeKey = h.edgeManager.GetKey()
	}

	dockerSnapshot, err := snapshot.DockerSnapshotDiagnostics(cli, edgeKey)
	if err != nil {
		return httperror.InternalServerError("Unable to create Docker diagnostics snapshot", err)
	}

	return response.JSON(rw, dockerSnapshot)
}
