package exec

import (
	"context"

	libstack "github.com/portainer/portainer/pkg/libstack"
	libstackerrors "github.com/portainer/portainer/pkg/libstack/errors"
)

func (service *KubernetesDeployer) WaitForStatus(ctx context.Context, name string, status libstack.Status) (<-chan string, <-chan error) {
	result := make(chan string)
	err := make(chan error)

	go func() {
		err <- libstackerrors.ErrNotImplemented

		close(result)
		close(err)
	}()

	return result, err
}
