package proxy

import (
	"io"
	"net/http"

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

	// The HTTP/2 server never sets an empty body to http.NoBody, which makes the
	// transport send it chunked. Docker rejects a chunked body on container start.
	if request.ContentLength == 0 {
		request.Body = http.NoBody
	}

	res, err := proxy.transport.RoundTrip(request)
	if err != nil {
		code := http.StatusInternalServerError
		if res != nil && res.StatusCode != 0 {
			code = res.StatusCode
		}

		httperror.WriteError(rw, code, "Unable to proxy the request via the Docker socket", err)

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
