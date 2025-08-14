package proxy

import (
	"net/http"
	"testing"

	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
)

func TestNewKubernetesProxy(t *testing.T) {
	fips.InitFIPS(false)

	proxy := NewKubernetesProxy()
	require.NotNil(t, proxy)
	require.True(t, proxy.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify) //nolint:forbidigo
}
