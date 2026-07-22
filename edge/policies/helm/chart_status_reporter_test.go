package helm_test

import (
	"sync"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/portainer/agent/edge/policies/helm"
)

func TestChartStatusReporter_SetAndSnapshot(t *testing.T) {
	t.Parallel()
	r := helm.NewChartStatusReporter()
	r.Set(1, []portainer.PolicyChartStatus{{ChartName: "gatekeeper", Fingerprint: "fp1"}})
	r.Set(2, []portainer.PolicyChartStatus{{ChartName: "portainer-registry-k8s", Fingerprint: "fp2"}})

	snap := r.Snapshot()
	require.Len(t, snap, 2)
}

func TestChartStatusReporter_ClearRemovesPolicy(t *testing.T) {
	t.Parallel()
	r := helm.NewChartStatusReporter()
	r.Set(1, []portainer.PolicyChartStatus{{ChartName: "gatekeeper"}})
	r.Set(2, []portainer.PolicyChartStatus{{ChartName: "portainer-registry-k8s"}})

	r.Clear(1)

	snap := r.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "portainer-registry-k8s", snap[0].ChartName)
}

func TestChartStatusSnapshot_NilReporter(t *testing.T) {
	t.Parallel()
	// Docker/Podman agents have a nil reporter — must not panic.
	result := helm.ChartStatusSnapshot(nil)
	assert.Nil(t, result)
}

func TestChartStatusReporter_ConcurrentAccess_NoRace(t *testing.T) {
	t.Parallel()
	r := helm.NewChartStatusReporter()
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			r.Set(portainer.PolicyID(i), []portainer.PolicyChartStatus{{ChartName: "chart"}})
		}()
		go func() {
			defer wg.Done()
			_ = r.Snapshot()
		}()
		go func() {
			defer wg.Done()
			r.Clear(portainer.PolicyID(i))
		}()
	}
	wg.Wait()
}
