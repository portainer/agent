package proxy

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
)

func TestNewAgentReverseProxy(t *testing.T) {
	fips.InitFIPS(false)

	u, err := url.Parse("http://localhost:9001")
	require.NoError(t, err)

	proxy := newAgentReverseProxy(u, "targetNode")
	require.NotNil(t, proxy)
	require.True(t, proxy.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify) //nolint:forbidigo
}
