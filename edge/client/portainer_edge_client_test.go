package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
)

func TestGetEdgeConfig(t *testing.T) {
	fips.InitFIPS(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Id": 1, "Name": "test"}`))
	}))
	defer srv.Close()

	client := &PortainerEdgeClient{
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      BuildHTTPClient(30, &agent.Options{}),
		serverAddress:   srv.URL,
	}

	edgeConfig, err := client.GetEdgeConfig(EdgeConfigID(1))
	require.NoError(t, err)
	require.Equal(t, edgeConfig.ID, EdgeConfigID(1))
	require.Equal(t, "test", edgeConfig.Name)
}
