package edge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/policies"
	"github.com/portainer/agent/edge/stack"
	"github.com/portainer/agent/policyreconcile"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPollAsync_NoEdgeID(t *testing.T) {
	t.Parallel()
	s := &PollService{edgeID: ""}

	err := s.pollAsync(true, true)
	require.Error(t, err)
}

func TestAsyncPollFlags(t *testing.T) {
	t.Parallel()

	require.Empty(t, asyncPollFlags(false, false))
	require.Equal(t, []string{"snapshot"}, asyncPollFlags(true, false))
	require.Equal(t, []string{"command"}, asyncPollFlags(false, true))
	require.Equal(t, []string{"snapshot", "command"}, asyncPollFlags(true, true))
}

// TestPollAsync_LostEndpointForgetsPolicyStateAndRearmsResync verifies that
// losing the endpoint association (a non-OK response from the poll request)
// makes the agent forget its local policy bookkeeping and request a fresh
// resync on the next command poll — the same recovery the agent gets from a
// full process restart.
func TestPollAsync_LostEndpointForgetsPolicyStateAndRearmsResync(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var (
		mu     sync.Mutex
		fail   bool
		resync bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		var req client.AsyncRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		resync = req.Resync

		assert.NoError(t, json.NewEncoder(w).Encode(&client.AsyncResponse{EndpointID: 1}))
	}))
	defer srv.Close()

	asyncClient := client.NewPortainerAsyncClient(
		srv.URL,
		func(id portainer.EndpointID) {},
		func() portainer.EndpointID { return 1 },
		"edge-id",
		"edge-key",
		agent.PlatformDocker,
		agent.EdgeMetaFields{},
		client.BuildHTTPClient(30, &agent.Options{}),
	)

	// Consume the resync that's armed by default at construction.
	_, err := asyncClient.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.True(t, resync)

	manager := NewManager(&ManagerParameters{Options: &agent.Options{DataPath: t.TempDir()}})
	manager.key = &edgeKey{EndpointID: 1, Global: true}

	service := &PollService{
		edgeID:           "edge-id",
		edgeManager:      manager,
		edgeStackManager: stack.NewStackManager(asyncClient, nil, "edge-id", nil),
		policyManager:    policies.NewPolicyManager(nil, nil, nil, nil, nil, nil, 0),
		reconciler:       policyreconcile.NewReconciler(),
		portainerClient:  asyncClient,
		firstPoll:        false,
		policies:         map[string]string{"stale-chart": "stale-fp"},
	}

	mu.Lock()
	fail = true
	mu.Unlock()

	err = service.pollAsync(false, true)
	require.Error(t, err, "a non-OK response must still be surfaced as an error")
	require.Nil(t, service.policies, "losing the endpoint must forget the legacy chart fingerprint cache")

	mu.Lock()
	fail = false
	resync = false
	mu.Unlock()

	_, err = asyncClient.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.True(t, resync, "losing the endpoint must rearm the resync request")
}
