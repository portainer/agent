package proxy

import (
	"net/http"
	"os"
	"testing"

	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const devKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://kubernetes.example.com:6443
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: test-token
`

func TestNewKubernetesProxy(t *testing.T) {
	fips.InitFIPS(false)

	proxy := NewKubernetesProxy()
	require.NotNil(t, proxy)
	require.True(t, proxy.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify) //nolint:forbidigo
}

func TestForceHTTP1(t *testing.T) {
	t.Parallel()

	config := &rest.Config{}
	forceHTTP1(config)

	require.Equal(t, []string{http1ProtoName}, config.TLSClientConfig.NextProtos)
}

func TestNewKubernetesProxyWithDevKubeconfig(t *testing.T) {
	fips.InitFIPS(false)

	f, err := os.CreateTemp(t.TempDir(), "kubeconfig-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(devKubeconfig)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Setenv("DEV_KUBECONFIG_PATH", f.Name())

	proxy := NewKubernetesProxy()
	require.NotNil(t, proxy)
	require.NotNil(t, proxy.Transport)
}
