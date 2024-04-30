package exec

import (
	"context"

	libstack "github.com/portainer/portainer/pkg/libstack"
)

func (service *KubernetesDeployer) WaitForStatus(ctx context.Context, name string, status libstack.Status) <-chan libstack.WaitResult {
	resultCh := make(chan libstack.WaitResult)

	close(resultCh)

	return resultCh
}
