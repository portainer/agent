//go:build !windows

package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/portainer/portainer/api/filesystem"

	"github.com/stretchr/testify/require"
)

type dockerRequest struct {
	contentLength    int64
	transferEncoding []string
	body             string
	readBodyErr      error
}

// startDockerSocket serves a stand-in for the Docker daemon on a Unix socket that records exactly what it receives.
func startDockerSocket(t *testing.T) (string, *dockerRequest) {
	t.Helper()

	socketPath := filesystem.JoinPaths(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	received := &dockerRequest{}
	docker := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.contentLength = r.ContentLength
		received.transferEncoding = r.TransferEncoding

		body, err := io.ReadAll(r.Body)
		received.readBodyErr = err
		received.body = string(body)

		w.WriteHeader(http.StatusNoContent)
	}))
	docker.Listener = listener
	docker.Start()
	t.Cleanup(docker.Close)

	return socketPath, received
}

// Portainer reaches the agent over HTTP/2, where an empty request body is never http.NoBody.
// The proxy must still forward it to Docker as a zero-length body rather than a chunked one,
// which Docker rejects on container start, while forwarding a streamed body unchanged.
func TestLocalProxy_ForwardsBodyOverHTTP2(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                     string
		body                     io.Reader
		expectedContentLength    int64
		expectedTransferEncoding []string
		expectedBody             string
	}{
		{
			name:                  "empty body is sent with zero length",
			body:                  nil,
			expectedContentLength: 0,
			expectedBody:          "",
		},
		{
			// Wrapping the reader hides its length, so the client streams it without a Content-Length
			name:                     "streamed body is forwarded intact",
			body:                     io.NopCloser(strings.NewReader("image-archive")),
			expectedContentLength:    -1,
			expectedTransferEncoding: []string{"chunked"},
			expectedBody:             "image-archive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			socketPath, received := startDockerSocket(t)

			agent := httptest.NewUnstartedServer(&LocalProxy{transport: newSocketTransport(socketPath)})
			agent.EnableHTTP2 = true
			agent.StartTLS()
			t.Cleanup(agent.Close)

			req, err := http.NewRequest(http.MethodPost, agent.URL+"/containers/abc/start", tc.body)
			require.NoError(t, err)

			res, err := agent.Client().Do(req)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, res.Body.Close()) })

			require.Equal(t, 2, res.ProtoMajor, "the request should reach the agent over HTTP/2")
			require.Equal(t, http.StatusNoContent, res.StatusCode)

			require.NoError(t, received.readBodyErr)
			require.Equal(t, tc.expectedContentLength, received.contentLength)
			require.Equal(t, tc.expectedTransferEncoding, received.transferEncoding)
			require.Equal(t, tc.expectedBody, received.body)
		})
	}
}
