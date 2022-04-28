package edge

import (
	"os"
	"testing"
	"time"

	"github.com/portainer/agent"
)

func TestKeyDataRace(t *testing.T) {
	mgr := NewManager(&ManagerParameters{
		Options: &agent.Options{
			DataPath: os.TempDir(),
		},
	})

	go func() {
		mgr.SetKey(encodeKey(&edgeKey{}))
	}()

	time.Sleep(1 * time.Second)
	mgr.IsKeySet()
	time.Sleep(1 * time.Second)
}
