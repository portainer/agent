package exec

import (
	"context"

	libstack "github.com/portainer/portainer/pkg/libstack"
)

func (service *KubernetesDeployer) WaitForStatus(ctx context.Context, name string, status libstack.Status) <-chan string {
	resultCh := make(chan string)

	close(resultCh)

	return resultCh
}
