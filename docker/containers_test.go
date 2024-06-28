package docker

import (
	"os"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestContainerWait(t *testing.T) {
	require.NoError(t, os.Setenv("DOCKER_HOST", "invalid-host"))

	statusCh, errCh := ContainerWait("container-name", container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return
		}
	case <-statusCh:
	}

	t.Fail()
}
