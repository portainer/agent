package proxy

import (
	"net/http"
	"testing"

	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
)

func TestNewClusterProxy(t *testing.T) {
	fips.InitFIPS(false)

	proxy := NewClusterProxy(true)
	require.NotNil(t, proxy)
	require.True(t, proxy.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify) //nolint:forbidigo
}
