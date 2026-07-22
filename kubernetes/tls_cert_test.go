package kubernetes

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/portainer/portainer/pkg/fips"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func init() {
	fips.InitFIPS(false)
}

func TestCollectAPIServerCert(t *testing.T) {
	t.Parallel()

	t.Run("returns error when kc is nil", func(t *testing.T) {
		t.Parallel()

		_, err := CollectAPIServerCert(context.Background(), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "kubernetes client is nil")
	})

	t.Run("returns error when kc.config is nil", func(t *testing.T) {
		t.Parallel()

		_, err := CollectAPIServerCert(context.Background(), &KubeClient{})
		require.Error(t, err)
		assert.ErrorContains(t, err, "kubernetes client config is nil")
	})

	t.Run("returns cert details when API server cert is trusted", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		caData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})

		kc := &KubeClient{config: &rest.Config{
			Host: server.URL,
			TLSClientConfig: rest.TLSClientConfig{
				CAData: caData,
			},
		}}

		cert, err := CollectAPIServerCert(context.Background(), kc)
		require.NoError(t, err)
		require.NotNil(t, cert)

		expectedCN := strings.TrimSpace(server.Certificate().Subject.CommonName)
		if expectedCN == "" {
			expectedCN = "unknown"
		}

		assert.Equal(t, "apiserver", cert.Source)
		assert.Equal(t, expectedCN, cert.CN)
		assert.WithinDuration(t, server.Certificate().NotAfter, cert.NotAfter, time.Second)
	})

	t.Run("accepts portless URL and fails at connection not validation", func(t *testing.T) {
		t.Parallel()

		kc := &KubeClient{config: &rest.Config{Host: "https://kubernetes.default.svc"}}

		_, err := CollectAPIServerCert(context.Background(), kc)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to dial kubernetes api server tls endpoint")
	})

	t.Run("returns cert details even when cert chain cannot be verified", func(t *testing.T) {
		t.Parallel()

		// InsecureSkipVerify is intentional: we inspect the cert's NotAfter field
		// regardless of whether the cert is trusted or expired.
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		kc := &KubeClient{config: &rest.Config{Host: server.URL}}

		cert, err := CollectAPIServerCert(context.Background(), kc)
		require.NoError(t, err)
		require.NotNil(t, cert)

		assert.Equal(t, "apiserver", cert.Source)
		assert.WithinDuration(t, server.Certificate().NotAfter, cert.NotAfter, time.Second)
	})
}

func TestBuildAPIServerTLSTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		host        string
		expected    string
		expectedErr string
	}{
		{
			name:     "defaults to 443 for dns host",
			host:     "https://kubernetes.default.svc",
			expected: "kubernetes.default.svc:443",
		},
		{
			name:     "keeps explicit port for dns host",
			host:     "https://kubernetes.default.svc:6443",
			expected: "kubernetes.default.svc:6443",
		},
		{
			name:     "defaults to 443 for ipv6 host",
			host:     "https://[2001:db8::1]",
			expected: "[2001:db8::1]:443",
		},
		{
			name:     "keeps explicit port for ipv6 host",
			host:     "https://[2001:db8::1]:6443",
			expected: "[2001:db8::1]:6443",
		},
		{
			name:        "invalid host",
			host:        "https://",
			expectedErr: "kubernetes api server host is invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsedURL, err := resolveAPIServerURL(tc.host)
			if tc.expectedErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.expectedErr)

				return
			}

			require.NoError(t, err)

			target, err := buildAPIServerTLSTarget(parsedURL)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, target)
		})
	}
}
