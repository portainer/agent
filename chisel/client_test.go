package chisel

import (
	"os"
	"testing"
	"time"

	chclient "github.com/jpillora/chisel/client"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/pkg/fips"
	"github.com/stretchr/testify/require"
)

func init() {
	fips.InitFIPS(false)
}

func TestCertsNeedRotation(t *testing.T) {
	t.Parallel()
	c := NewClient("", "", "")
	certsNeedRotation := c.CertsNeedRotation()
	require.False(t, certsNeedRotation)
}

func TestInternalCertsNeedRotation(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name                      string
		fips                      bool
		modifyFiles               bool
		expectedCertsNeedRotation bool
	}{
		{
			name:                      "not fips",
			fips:                      false,
			modifyFiles:               false,
			expectedCertsNeedRotation: false,
		},
		{
			name:                      "fips and files not modified",
			fips:                      true,
			modifyFiles:               false,
			expectedCertsNeedRotation: false,
		},
		{
			name:                      "fips and files modified",
			fips:                      true,
			modifyFiles:               true,
			expectedCertsNeedRotation: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			tlsCaCert := filesystem.JoinPaths(dir, "tls-ca-cert.pem")
			err := os.WriteFile(tlsCaCert, []byte{}, 0o600)
			require.NoError(t, err)

			tlsCert := filesystem.JoinPaths(dir, "tls-cert.pem")
			err = os.WriteFile(tlsCert, []byte{}, 0o600)
			require.NoError(t, err)

			tlsKey := filesystem.JoinPaths(dir, "tls-key.pem")
			err = os.WriteFile(tlsKey, []byte{}, 0o600)
			require.NoError(t, err)

			if tc.modifyFiles {
				// Set file times to the past so that subsequent writes produce a detectable mtime change.
				past := time.Now().Add(-2 * time.Second)
				require.NoError(t, os.Chtimes(tlsCaCert, past, past))
				require.NoError(t, os.Chtimes(tlsCert, past, past))
				require.NoError(t, os.Chtimes(tlsKey, past, past))
			}

			c := newClient(tlsCaCert, tlsCert, tlsKey, tc.fips)

			if tc.modifyFiles {
				err := os.WriteFile(tlsCaCert, []byte("new-data"), 0o600)
				require.NoError(t, err)

				err = os.WriteFile(tlsCert, []byte{}, 0o600)
				require.NoError(t, err)

				err = os.WriteFile(tlsKey, []byte{}, 0o600)
				require.NoError(t, err)
			}

			certsNeedRotation := c.certsNeedRotation(tc.fips)
			require.Equal(t, tc.expectedCertsNeedRotation, certsNeedRotation)
		})
	}
}

func TestCloseTunnel(t *testing.T) {
	t.Parallel()
	c := NewClient("", "", "")

	chiselClient, err := chclient.NewClient(&chclient.Config{})
	require.NoError(t, err)

	c.chiselClient = chiselClient

	err = c.CloseTunnel()
	require.NoError(t, err)
}

func TestReplaceSchemaWithHTTPS(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		u           string
		expectedURL string
	}{
		{
			name:        "empty url",
			u:           "",
			expectedURL: "https://",
		},
		{
			name:        "no schema",
			u:           "localhost:8888",
			expectedURL: "https://localhost:8888",
		},
		{
			name:        "http schema",
			u:           "http://localhost:8888",
			expectedURL: "https://localhost:8888",
		},
		{
			name:        "https schema",
			u:           "https://localhost:8888",
			expectedURL: "https://localhost:8888",
		},
		{
			name:        "ws schema",
			u:           "ws://localhost:8888",
			expectedURL: "https://localhost:8888",
		},
		{
			name:        "wss schema",
			u:           "wss://localhost:8888",
			expectedURL: "wss://localhost:8888",
		},
		{
			name:        "other schema",
			u:           "dsalf://localhost:8888",
			expectedURL: "https://dsalf://localhost:8888",
		},
		{
			name:        "leading and trailing space with no schema",
			u:           " localhost:8888 ",
			expectedURL: "https://localhost:8888",
		},
		{
			name:        "leading and trailing space with http schema",
			u:           " http://localhost:8888 ",
			expectedURL: "https://localhost:8888",
		},
		{
			name:        "leading and trailing space with https schema",
			u:           " https://localhost:8888 ",
			expectedURL: "https://localhost:8888",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotURL := replaceSchemaWithHTTPS(tc.u)

			require.Equal(t, tc.expectedURL, gotURL)
		})
	}
}
