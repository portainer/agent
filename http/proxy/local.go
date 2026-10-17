package proxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"

	"github.com/rs/zerolog/log"
)

// LocalProxy is a service used to proxy requests to a Unix socket (Linux) or named pipe (Windows).
// The proxy operation implementation is defined in the ServeHTTP function.
type LocalProxy struct {
	transport *http.Transport
}

func (proxy *LocalProxy) ServeHTTP(rw http.ResponseWriter, request *http.Request) {
	request.URL.Scheme = "http"
	request.URL.Host = "unixsocket"

	res, err := proxy.transport.RoundTrip(request)
	if err != nil {
		code := http.StatusInternalServerError
		if res != nil && res.StatusCode != 0 {
			code = res.StatusCode
		}

		httperror.WriteError(rw, code, "Unable to proxy the request via the Docker socket", err)

		return
	}

	// The Docker daemon switches protocols for some endpoints, e.g. the h2c upgrade of BuildKit's /session
	// and /grpc endpoints used by `docker build` and `docker compose build`. The upgraded connection must be
	// relayed as a raw byte stream instead of being copied as a response body.
	if res.StatusCode == http.StatusSwitchingProtocols {
		serveProtocolSwitch(rw, res)

		return
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("Failed closing proxied request body")
		}
	}()

	for k, vv := range res.Header {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}

	rw.WriteHeader(res.StatusCode)

	// TODO: resource duplication error: it seems that the body size is different here
	// from the size retrieve in cluster.go
	if _, err := io.Copy(rw, res.Body); err != nil {
		log.Warn().Err(err).Msg("Error copying response body from proxied request")
	}
}

// serveProtocolSwitch relays a "101 Switching Protocols" response to the client, then copies bytes in both
// directions between the hijacked client connection and the upgraded Docker daemon connection until either
// side closes.
func serveProtocolSwitch(rw http.ResponseWriter, res *http.Response) {
	// For a 101 response, http.Transport returns the upgraded connection as the response body
	backendConn, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		closeConn(res.Body)
		httperror.WriteError(rw, http.StatusBadGateway, "Unable to proxy the request via the Docker socket", errors.New("upgraded Docker connection is not writable"))

		return
	}
	defer closeConn(backendConn)

	clientConn, clientBuf, err := http.NewResponseController(rw).Hijack()
	if err != nil {
		httperror.WriteError(rw, http.StatusInternalServerError, "Unable to proxy the request via the Docker socket", err)

		return
	}
	defer closeConn(clientConn)

	// The server read/write timeouts were set for ordinary requests and must not cut off the
	// long-lived upgraded connection
	if err := clientConn.SetDeadline(time.Time{}); err != nil {
		log.Warn().Err(err).Msg("Failed clearing deadline on upgraded connection")

		return
	}

	if _, err := fmt.Fprintf(clientBuf, "HTTP/1.1 %s\r\n", res.Status); err != nil {
		log.Warn().Err(err).Msg("Failed writing protocol switch response")

		return
	}

	if err := res.Header.Write(clientBuf); err != nil {
		log.Warn().Err(err).Msg("Failed writing protocol switch response headers")

		return
	}

	if _, err := clientBuf.WriteString("\r\n"); err != nil {
		log.Warn().Err(err).Msg("Failed writing protocol switch response")

		return
	}

	if err := clientBuf.Flush(); err != nil {
		log.Warn().Err(err).Msg("Failed flushing protocol switch response")

		return
	}

	// Copy until either direction ends; the deferred closes then unblock the other direction.
	// Reading the client side through clientBuf.Reader includes any bytes it already buffered.
	errc := make(chan error, 2)

	go func() {
		_, err := io.Copy(backendConn, clientBuf.Reader)
		errc <- err
	}()

	go func() {
		_, err := io.Copy(clientConn, backendConn)
		errc <- err
	}()

	if err := <-errc; err != nil && !errors.Is(err, net.ErrClosed) {
		log.Debug().Err(err).Msg("Upgraded connection closed with error")
	}
}

func closeConn(c io.Closer) {
	if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Debug().Err(err).Msg("Failed closing upgraded connection")
	}
}
