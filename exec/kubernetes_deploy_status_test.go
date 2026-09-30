package exec

import (
	"os"
	"testing"

	"github.com/portainer/agent/deployer"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/stretchr/testify/require"
)

func TestKubernetesDeployer_getStatusForYAML_EmptyWorkloads(t *testing.T) {
	t.Parallel()

	manifestPath := filesystem.JoinPaths(t.TempDir(), "stack.yaml")
	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`
	err := os.WriteFile(manifestPath, []byte(manifest), 0644)
	require.NoError(t, err)

	service := &KubernetesDeployer{}
	options := deployer.CheckStatusOptions{
		DeployerBaseOptions: deployer.DeployerBaseOptions{Namespace: "default"},
		StackFileLocation:   manifestPath,
	}

	f := func(requiredStatus, expectedStatus libstack.Status) {
		t.Helper()

		status, message, err := service.getStatusForYAML(requiredStatus, options)
		require.NoError(t, err)

		require.Equal(t, expectedStatus, status)
		require.Empty(t, message)
	}

	// While waiting for removal, no matching workloads means the manifest is already removed
	f(libstack.StatusRemoved, libstack.StatusRemoved)

	// The deployment path is unaffected. An empty list still aggregates to Completed
	f(libstack.StatusRunning, libstack.StatusCompleted)
}
