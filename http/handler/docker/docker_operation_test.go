package docker

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	"github.com/portainer/agent/internals/mocks"
	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

func init() {
	fips.InitFIPS(false)
}

// A Docker daemon newer than the client's max API version makes the browser send
// version-prefixed paths (e.g. /v1.47/networks). The agent must trim that prefix
// before dispatching, otherwise list operations miss the cluster-aggregation
// branch and networks/volumes come back without their per-node NodeName. This
// test locks that behaviour: every list operation must reach the cluster branch
// whether or not the path carries a version prefix.
func TestDispatchOperation_routesListOperationsToClusterRegardlessOfVersion(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		path   string
	}{
		{"networks unversioned", http.MethodGet, "/networks"},
		{"networks versioned", http.MethodGet, "/v1.47/networks"},
		{"containers unversioned", http.MethodGet, "/containers/json"},
		{"containers versioned", http.MethodGet, "/v1.47/containers/json"},
		{"images unversioned", http.MethodGet, "/images/json"},
		{"images versioned", http.MethodGet, "/v1.47/images/json"},
		{"volumes unversioned", http.MethodGet, "/volumes"},
		{"volumes versioned", http.MethodGet, "/v1.47/volumes"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockCS := mocks.NewMockClusterService(ctrl)
			// Reaching the cluster-aggregation branch is the only path that calls
			// Members(); the single-node default branch never does. Requiring
			// exactly one call is therefore proof the request was aggregated.
			// Returning no members keeps ClusterOperation from making any network
			// calls while still exercising the routing decision.
			mockCS.EXPECT().Members().Return([]agent.ClusterMember{}).Times(1)

			handler := &Handler{
				dockerProxy:          proxy.NewLocalProxy(),
				clusterProxy:         proxy.NewClusterProxy(false),
				clusterService:       mockCS,
				runtimeConfiguration: &agent.RuntimeConfig{NodeName: "self"},
				useTLS:               false,
			}

			// No X-PortainerAgent-Target header: the target is not this node, so
			// the cluster branch proceeds to Members() rather than serving locally.
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			err := handler.dispatchOperation(rec, req)
			require.Nil(t, err)
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}
