package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newDockerBackend returns a test server that stands in for the Docker daemon. Requests to /grpc are
// upgraded to "h2c" the way the daemon does for BuildKit sessions, after which the server echoes every
// byte it receives. Other requests receive a plain "ok" response.
func newDockerBackend(t *testing.T) *httptest.Server {
	t.Helper()

	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/grpc" {
			_, _ = io.WriteString(rw, "ok")

			return
		}

		conn, buf, err := http.NewResponseController(rw).Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)

			return
		}
		defer conn.Close()

		// Bound the echo so that a proxy that never relays the upgraded connection fails the test
		// instead of hanging it
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		_, _ = io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n")
		_, _ = io.Copy(conn, buf)
	}))
	t.Cleanup(backend.Close)

	return backend
}

func newTestLocalProxy(backend *httptest.Server) *LocalProxy {
	return &LocalProxy{
		transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer

				return d.DialContext(ctx, "tcp", backend.Listener.Addr().String())
			},
		},
	}
}

func TestLocalProxyRelaysProtocolSwitch(t *testing.T) {
	backend := newDockerBackend(t)

	// Use short server timeouts to verify that they do not apply to the upgraded connection
	front := httptest.NewUnstartedServer(newTestLocalProxy(backend))
	front.Config.ReadTimeout = 200 * time.Millisecond
	front.Config.WriteTimeout = 200 * time.Millisecond
	front.Start()
	t.Cleanup(front.Close)

	conn, err := net.Dial("tcp", front.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = io.WriteString(conn, "POST /grpc HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n")
	require.NoError(t, err)

	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, res.StatusCode)
	require.Equal(t, "101 UPGRADED", res.Status)
	require.Equal(t, "h2c", res.Header.Get("Upgrade"))

	// Wait past the server timeouts before using the upgraded connection
	time.Sleep(400 * time.Millisecond)

	// Fail instead of hanging if the proxy does not relay the upgraded connection
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))

	for _, msg := range []string{"ping\n", "pong\n"} {
		_, err = io.WriteString(conn, msg)
		require.NoError(t, err)

		echoed, err := reader.ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, msg, echoed)
	}
}

func TestLocalProxyCopiesOrdinaryResponse(t *testing.T) {
	backend := newDockerBackend(t)

	front := httptest.NewServer(newTestLocalProxy(backend))
	t.Cleanup(front.Close)

	res, err := http.Get(front.URL + "/version")
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "ok", string(body))
}
