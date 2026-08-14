package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClusterProxy(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	proxy := NewClusterProxy(true)
	require.NotNil(t, proxy)
	require.True(t, proxy.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify) //nolint:forbidigo
}

// TestClusterOperation_ConcurrentMembersDoNotRaceOnRequestURL guards against a data
// race in copyRequest: request.URL is a *url.URL shared by every per-member goroutine
// spawned in executeRequestOnCluster, so copying the pointer (instead of the value)
// and then mutating Host/Scheme on it races across goroutines. Run with -race.
func TestClusterOperation_ConcurrentMembersDoNotRaceOnRequestURL(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	const memberCount = 5

	clusterMembers := make([]agent.ClusterMember, memberCount)

	for i := range memberCount {
		mux := http.NewServeMux()
		mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			_, err := w.Write([]byte(`[]`))
			assert.NoError(t, err)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		require.NoError(t, err)

		clusterMembers[i] = agent.ClusterMember{NodeName: "worker", IPAddress: host, Port: port}
	}

	request := httptest.NewRequest(http.MethodGet, "/containers/json", nil)

	clusterProxy := NewClusterProxy(false)

	_, err := clusterProxy.ClusterOperation(request, clusterMembers)
	require.NoError(t, err)
}
