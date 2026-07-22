package edge

import (
	"sync"
	"testing"

	"github.com/portainer/agent"
)

func TestKeyDataRace(t *testing.T) {
	t.Parallel()
	mgr := NewManager(&ManagerParameters{
		Options: &agent.Options{
			DataPath: t.TempDir(),
		},
	})

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = mgr.SetKey(encodeKey(&edgeKey{}))
	})

	mgr.IsKeySet()
	wg.Wait()
}
