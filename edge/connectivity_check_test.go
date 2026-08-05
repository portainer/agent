package edge

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agent "github.com/portainer/agent"
	"github.com/portainer/portainer/api/crypto"
)

func makeEdgeKey(portainerURL, tunnelAddr, fingerprint string, endpointID int) string {
	raw := fmt.Sprintf("%s|%s|%s|%d", portainerURL, tunnelAddr, fingerprint, endpointID)
	return base64.RawStdEncoding.EncodeToString([]byte(raw))
}

func defaultOptions() *agent.Options {
	return &agent.Options{
		EdgeTunnel: true,
	}
}

func TestResolveCheckParams_ExplicitURL(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "http://portainer:9000"
	opts.EdgeConnectivityCheckTunnel = "tunnel:8000"

	params, err := resolveCheckParams(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if params.portainerURL != "http://portainer:9000" {
		t.Errorf("expected portainerURL http://portainer:9000, got %s", params.portainerURL)
	}
	if params.tunnelAddr != "tunnel:8000" {
		t.Errorf("expected tunnelAddr tunnel:8000, got %s", params.tunnelAddr)
	}
	if params.skipTunnel {
		t.Error("expected skipTunnel=false when tunnel addr is provided and not async")
	}
}

func TestResolveCheckParams_ExplicitURL_NoTunnel(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "http://portainer:9000"

	params, err := resolveCheckParams(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !params.skipTunnel {
		t.Error("expected skipTunnel=true when no tunnel addr")
	}
}

func TestResolveCheckParams_EdgeKey(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeKey = makeEdgeKey("http://portainer:9000", "tunnel:8000", "fp", 1)

	params, err := resolveCheckParams(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if params.portainerURL != "http://portainer:9000" {
		t.Errorf("expected portainerURL http://portainer:9000, got %s", params.portainerURL)
	}
	if params.tunnelAddr != "tunnel:8000" {
		t.Errorf("expected tunnelAddr tunnel:8000, got %s", params.tunnelAddr)
	}
}

func TestResolveCheckParams_InvalidEdgeKey(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeKey = "not-valid-base64!!!"

	_, err := resolveCheckParams(opts)
	if err == nil {
		t.Error("expected error for invalid edge key")
	}
}

func TestResolveCheckParams_NoTargets(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()

	_, err := resolveCheckParams(opts)
	if err == nil {
		t.Error("expected error when no URL and no edge key")
	}
}

func TestResolveCheckParams_InvalidExplicitURL(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "portainer:9000"

	_, err := resolveCheckParams(opts)
	assertErrorContains(t, err, "EDGE_CONNECTIVITY_CHECK_URL must include http:// or https://")
}

func TestResolveCheckParams_BareHostTunnelAddr(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "https://portainer.example.com"
	opts.EdgeConnectivityCheckTunnel = "tunnel.example.com"

	params, err := resolveCheckParams(opts)
	if err != nil {
		t.Fatalf("bare FQDN tunnel addr should be accepted, got: %v", err)
	}

	if params.tunnelAddr != "tunnel.example.com" {
		t.Errorf("expected tunnelAddr %q, got %q", "tunnel.example.com", params.tunnelAddr)
	}
}

func TestResolveCheckParams_InvalidProxyURL(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "https://portainer.example.com"
	opts.EdgeTunnelProxy = "proxy.example.com:8080"

	_, err := resolveCheckParams(opts)
	assertErrorContains(t, err, "proxy URL must include http:// or https://")
}

func TestResolveCheckParams_InvalidMTLSCAPath(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "https://portainer.example.com"
	opts.SSLCert = filepath.Join(t.TempDir(), "client.crt")
	opts.SSLKey = filepath.Join(t.TempDir(), "client.key")
	opts.SSLCACert = filepath.Join(t.TempDir(), "ca.crt")

	_, err := resolveCheckParams(opts)
	assertErrorContains(t, err, "failed to read MTLS_SSL_CA file")
}

func TestResolveCheckParams_PartialMTLSConfig(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "https://portainer.example.com"
	opts.SSLCert = "/certs/client.crt"

	_, err := resolveCheckParams(opts)
	assertErrorContains(t, err, "mTLS requires MTLS_SSL_CERT and MTLS_SSL_KEY to be set together")
}

func TestCheckConnectivity_APIPass(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true, got %v", ok)
	}
}

func TestCheckConnectivity_APIPassAnyStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true for HTTP 401 (TCP+TLS works), got %v", ok)
	}
}

func TestCheckConnectivity_APIPassWithMTLS(t *testing.T) {
	t.Parallel()
	certs := createMTLSCerts(t)

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(certs.caCertPEM) {
		t.Fatal("failed to add test CA certificate")
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	tlsConfig := crypto.CreateTLSConfiguration(false)
	tlsConfig.Certificates = []tls.Certificate{certs.serverCert}
	tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	tlsConfig.ClientCAs = caPool
	srv.TLS = tlsConfig
	srv.StartTLS()
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.SSLCACert = certs.caCertPath
	opts.SSLCert = certs.clientCertPath
	opts.SSLKey = certs.clientKeyPath

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true with mTLS certs configured, got %v", ok)
	}
}

func TestCheckConnectivity_APIFail(t *testing.T) {
	t.Parallel()
	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "http://" + unusedLocalAddr(t)

	ok := HasServerConnectivity(opts)
	if ok {
		t.Errorf("expected false, got %v", ok)
	}
}

func TestCheckConnectivity_TunnelPass(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tunnelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer tunnelSrv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = tunnelSrv.Listener.Addr().String()
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true, got %v", ok)
	}
}

func TestCheckConnectivity_TunnelPassWithMalformedHTTPResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tunnelAddr := malformedHTTPHeaderServerAddr(t)

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = tunnelAddr
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true when the tunnel port is reachable but returns a malformed HTTP response, got %v", ok)
	}
}

func TestCheckConnectivity_WhitespaceTunnelAddrSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = "   "
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true when tunnel address is whitespace-only (treated as absent), got %v", ok)
	}
}

func TestCheckConnectivity_TunnelFail(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = unusedLocalAddr(t)
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if ok {
		t.Errorf("expected false when tunnel unreachable, got %v", ok)
	}
}

func TestCheckConnectivity_TunnelFailOnUnexpectedProbeError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tunnelAddr := abruptCloseServerAddr(t)

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = tunnelAddr
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if ok {
		t.Errorf("expected false when tunnel probe returns unexpected error, got %v", ok)
	}
}

func TestCheckConnectivity_AsyncSkipsTunnel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = "127.0.0.1:19997" // not listening, but should be skipped
	opts.EdgeAsyncMode = true
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true (tunnel skipped for async), got %v", ok)
	}
}

func TestCheckConnectivity_TunnelDisabledSkips(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = srv.URL
	opts.EdgeConnectivityCheckTunnel = "127.0.0.1:19996" // not listening, but should be skipped
	opts.EdgeTunnel = false
	opts.EdgeInsecurePoll = true

	ok := HasServerConnectivity(opts)
	if !ok {
		t.Errorf("expected true (tunnel skipped when EdgeTunnel=false), got %v", ok)
	}
}

type mtlsTestCerts struct {
	caCertPEM      []byte
	serverCert     tls.Certificate
	caCertPath     string
	clientCertPath string
	clientKeyPath  string
}

func createMTLSCerts(t *testing.T) mtlsTestCerts {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	serverCertPEM, serverKeyPEM := createSignedCert(
		t,
		caCert,
		caKey,
		2,
		"test-server",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]net.IP{net.ParseIP("127.0.0.1")},
	)
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}

	clientCertPEM, clientKeyPEM := createSignedCert(
		t,
		caCert,
		caKey,
		3,
		"test-client",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		nil,
	)

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	clientCertPath := filepath.Join(dir, "client.crt")
	clientKeyPath := filepath.Join(dir, "client.key")

	writeTestFile(t, caPath, caPEM)
	writeTestFile(t, clientCertPath, clientCertPEM)
	writeTestFile(t, clientKeyPath, clientKeyPEM)

	return mtlsTestCerts{
		caCertPEM:      caPEM,
		serverCert:     serverCert,
		caCertPath:     caPath,
		clientCertPath: clientCertPath,
		clientKeyPath:  clientKeyPath,
	}
}

func createSignedCert(
	t *testing.T,
	caCert *x509.Certificate,
	caKey *rsa.PrivateKey,
	serial int64,
	commonName string,
	extKeyUsage []x509.ExtKeyUsage,
	ipAddresses []net.IP,
) ([]byte, []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  extKeyUsage,
		IPAddresses:  ipAddresses,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certPEM, keyPEM
}

func writeTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()

	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func unusedLocalAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	return addr
}

func malformedHTTPHeaderServerAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Read the request before replying so the connection closes cleanly.
			// Closing with the request unread resets the connection, discarding the
			// malformed reply so the client sees a reset instead of a parse error.
			_, _ = http.ReadRequest(bufio.NewReader(conn))
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type\r\n\r\n"))
			_ = conn.Close()
		}
	}()

	return listener.Addr().String()
}

func abruptCloseServerAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Read the request before closing so the connection closes cleanly.
			// Closing with the request unread resets the connection, which the
			// client sees as a reset instead of the EOF this test exercises.
			_, _ = http.ReadRequest(bufio.NewReader(conn))
			_ = conn.Close()
		}
	}()

	return listener.Addr().String()
}

// startUnresponsiveListener accepts TCP connections but never answers them, which is
// what a firewalled port or a wildcard DNS record does. The probe then stalls until
// its own timeout rather than failing fast.
func startUnresponsiveListener(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// hold the connection open without replying
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	return listener.Addr().String()
}

// captureStdout collects everything HasServerConnectivity prints, so tests can assert
// on what the user actually sees.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	original := os.Stdout
	os.Stdout = writer

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, reader)
		done <- sb.String()
	}()

	fn()

	os.Stdout = original
	_ = writer.Close()
	output := <-done
	_ = reader.Close()

	return output
}

func TestResolveCheckTimeout_DefaultWhenUnset(t *testing.T) {
	t.Parallel()

	if timeout := resolveCheckTimeout(defaultOptions()); timeout != DefaultConnectivityCheckTimeoutSeconds {
		t.Errorf("expected default timeout %d, got %v", DefaultConnectivityCheckTimeoutSeconds, timeout)
	}
}

func TestResolveCheckTimeout_HonoursOverride(t *testing.T) {
	t.Parallel()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckTimeout = 45

	if timeout := resolveCheckTimeout(opts); timeout != 45 {
		t.Errorf("expected timeout 45, got %v", timeout)
	}
}

// C9S-235: an unresponsive target used to leave the user staring at a blank console
// for the full edge client timeout. The check must announce each target up front and
// give up after its own configured timeout.
func TestCheckConnectivity_AnnouncesTargetBeforeStallingProbe(t *testing.T) {
	addr := startUnresponsiveListener(t)

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = "http://" + addr
	opts.EdgeConnectivityCheckTimeout = 1
	opts.EdgeInsecurePoll = true

	var passed bool
	start := time.Now()
	output := captureStdout(t, func() { passed = HasServerConnectivity(opts) })
	elapsed := time.Since(start)

	if passed {
		t.Error("expected the check to fail against an unresponsive target")
	}

	if !strings.Contains(output, "CHECK (1/1): trying to reach the Portainer API server at http://"+addr) {
		t.Errorf("expected the target to be announced before the probe, got:\n%s", output)
	}

	if !strings.Contains(output, "waiting up to 1s") {
		t.Errorf("expected the announced wait to reflect the configured timeout, got:\n%s", output)
	}

	if !strings.Contains(output, "FAIL: Failed to reach Portainer API server") {
		t.Errorf("expected a FAIL line, got:\n%s", output)
	}

	if !strings.Contains(output, "RESULT: connectivity checks failed") {
		t.Errorf("expected a closing RESULT line, got:\n%s", output)
	}

	// guards against the probe falling back to the 30s edge client default
	if elapsed > 15*time.Second {
		t.Errorf("expected the probe to honour the 1s timeout, took %v", elapsed)
	}
}

func TestCheckConnectivity_PrintsPassingResultLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := defaultOptions()
	opts.EdgeConnectivityCheckURL = server.URL

	var passed bool
	output := captureStdout(t, func() { passed = HasServerConnectivity(opts) })

	if !passed {
		t.Errorf("expected the check to pass, got output:\n%s", output)
	}

	if !strings.Contains(output, "RESULT: all 1 connectivity checks passed") {
		t.Errorf("expected a closing RESULT line, got:\n%s", output)
	}
}

func assertErrorContains(t *testing.T, err error, expected string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error containing %q, got nil", expected)
	}

	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got %q", expected, err.Error())
	}
}
