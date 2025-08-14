package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
	"github.com/wI2L/jsondiff"
)

func Test_executeAsyncRequestCompression(t *testing.T) {
	fips.InitFIPS(false)

	client := &PortainerAsyncClient{
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      BuildHTTPClient(30, &agent.Options{}),
	}

	// Small payload, no compression expected
	payload := AsyncRequest{Snapshot: &snapshot{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEqual(t, "gzip", r.Header.Get("Content-Encoding"))

		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	_, err := client.executeAsyncRequest(payload, srv.URL)
	require.NoError(t, err)

	// Large payload, compression expected
	payload = AsyncRequest{Snapshot: &snapshot{}}
	payload.Snapshot.DockerPatch = make([]jsondiff.Operation, 100)

	for i := range payload.Snapshot.DockerPatch {
		payload.Snapshot.DockerPatch[i] = jsondiff.Operation{
			Type: jsondiff.OperationAdd,
		}
	}

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))

		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	_, err = client.executeAsyncRequest(payload, srv.URL)
	require.NoError(t, err)
}
