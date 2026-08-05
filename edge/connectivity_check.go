package edge

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	agent "github.com/portainer/agent"
	"github.com/portainer/agent/chisel"
	"github.com/portainer/agent/edge/client"
)

type connectivityCheckParams struct {
	portainerURL string
	tunnelAddr   string
	skipTunnel   bool
	timeout      float64
}

// DefaultConnectivityCheckTimeoutSeconds bounds how long a single connectivity
// probe waits. A firewalled port or a wildcard DNS record answers the SYN with
// silence, so without a bound each probe stalls for the full edge client
// timeout with nothing on screen.
const DefaultConnectivityCheckTimeoutSeconds = 15

func resolveCheckTimeout(options *agent.Options) float64 {
	if options.EdgeConnectivityCheckTimeout > 0 {
		return float64(options.EdgeConnectivityCheckTimeout)
	}

	return DefaultConnectivityCheckTimeoutSeconds
}

func resolveCheckParams(options *agent.Options) (*connectivityCheckParams, error) {
	if options == nil {
		return nil, errors.New("missing agent options")
	}

	if options.EdgeConnectivityCheckURL != "" {
		portainerURL, err := validatePortainerURL("EDGE_CONNECTIVITY_CHECK_URL", options.EdgeConnectivityCheckURL)
		if err != nil {
			return nil, err
		}

		tunnelAddr := strings.TrimSpace(options.EdgeConnectivityCheckTunnel)
		params := &connectivityCheckParams{
			portainerURL: portainerURL,
			tunnelAddr:   tunnelAddr,
			skipTunnel:   options.EdgeAsyncMode || !options.EdgeTunnel || tunnelAddr == "",
			timeout:      resolveCheckTimeout(options),
		}

		return validateCheckParams(params, options)
	}

	if options.EdgeKey == "" {
		return nil, errors.New("no connectivity check targets: set EDGE_CONNECTIVITY_CHECK_URL or EDGE_KEY")
	}

	key, err := ParseEdgeKey(options.EdgeKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EDGE_KEY: %w", err)
	}

	portainerURL, err := validatePortainerURL("EDGE_KEY Portainer API URL", key.PortainerInstanceURL)
	if err != nil {
		return nil, err
	}

	tunnelAddr := strings.TrimSpace(key.TunnelServerAddr)
	params := &connectivityCheckParams{
		portainerURL: portainerURL,
		tunnelAddr:   tunnelAddr,
		skipTunnel:   options.EdgeAsyncMode || !options.EdgeTunnel || tunnelAddr == "",
		timeout:      resolveCheckTimeout(options),
	}

	return validateCheckParams(params, options)
}

func validateCheckParams(params *connectivityCheckParams, options *agent.Options) (*connectivityCheckParams, error) {
	if options.EdgeTunnelProxy != "" {
		if err := validateProxyURL(options.EdgeTunnelProxy); err != nil {
			return nil, err
		}
	}

	if err := validateMTLSConfig(options); err != nil {
		return nil, err
	}

	return params, nil
}

func validatePortainerURL(name string, rawURL string) (string, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	if trimmedURL == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}

	if err := validateHTTPURL(name, trimmedURL); err != nil {
		return "", err
	}

	return strings.TrimRight(trimmedURL, "/"), nil
}

func validateProxyURL(proxyURL string) error {
	return validateHTTPURL("proxy URL", proxyURL)
}

func validateHTTPURL(name, rawURL string) error {
	if !strings.Contains(rawURL, "://") {
		return fmt.Errorf("%s must include http:// or https://", name)
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", name, err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s must use http:// or https://", name)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("%s must include a host", name)
	}

	return nil
}

func validateMTLSConfig(options *agent.Options) error {
	hasCert := options.SSLCert != ""
	hasKey := options.SSLKey != ""
	hasCA := options.SSLCACert != ""

	if !hasCert && !hasKey && !hasCA {
		return nil
	}

	if hasCA && (!hasCert || !hasKey) {
		return errors.New("mTLS requires MTLS_SSL_CERT and MTLS_SSL_KEY when MTLS_SSL_CA is set")
	}

	if hasCert != hasKey {
		return errors.New("mTLS requires MTLS_SSL_CERT and MTLS_SSL_KEY to be set together")
	}

	if hasCA {
		caCert, err := os.ReadFile(options.SSLCACert)
		if err != nil {
			return fmt.Errorf("failed to read MTLS_SSL_CA file %q: %w", options.SSLCACert, err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("MTLS_SSL_CA file %q must contain a PEM encoded CA certificate", options.SSLCACert)
		}
	}

	if hasCert {
		if _, err := tls.LoadX509KeyPair(options.SSLCert, options.SSLKey); err != nil {
			return fmt.Errorf("failed to load mTLS client certificate/key from MTLS_SSL_CERT and MTLS_SSL_KEY: %w", err)
		}
	}

	return nil
}

// HasServerConnectivity performs a one-shot connectivity check against the Portainer
// API server and, when configured, the tunnel server. Results are printed to stdout.
// Returns true if all checks pass, false if any fail.
func HasServerConnectivity(options *agent.Options) bool {
	params, err := resolveCheckParams(options)
	if err != nil {
		fmt.Printf("FAIL: Invalid connectivity check configuration: %v\n", err)
		return false
	}

	allPassed := true
	checkCount := 1
	totalChecks := 1
	if !params.skipTunnel && params.tunnelAddr != "" {
		totalChecks = 2
	}

	apiURL := params.portainerURL + "/api/system/status"
	httpClient := client.BuildHTTPClient(params.timeout, options)

	if options.EdgeTunnelProxy != "" {
		proxyURL, _ := url.Parse(options.EdgeTunnelProxy)
		httpClient.SetProxy(proxyURL)
	}

	// intentionally use fmt.Printf instead of log.x() calls, so that Pass/Fail connectivity results are most obvious and aren't hidden in log formatting
	// announce each check before running it, so a probe that stalls on an unresponsive
	// host still tells the user what it is waiting for instead of printing nothing
	printCheckStart(checkCount, totalChecks, "Portainer API server", params.portainerURL, params.timeout)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		fmt.Printf("FAIL: Failed to reach Portainer API server at %s: %v\n", params.portainerURL, err)
		allPassed = false
	} else {
		resp, err := httpClient.DoConnectivityCheck(req)
		if err != nil {
			fmt.Printf("FAIL: Failed to reach Portainer API server at %s: %v\n", params.portainerURL, err)
			allPassed = false
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			fmt.Printf("PASS: Portainer API server is reachable at %s\n", params.portainerURL)
		}
	}

	if !params.skipTunnel && params.tunnelAddr != "" {
		checkCount++
		printCheckStart(checkCount, totalChecks, "Portainer tunnel server", params.tunnelAddr, params.timeout)

		tunnelURL := chisel.TunnelProbeURL(params.tunnelAddr)
		req, err := http.NewRequest(http.MethodGet, tunnelURL, nil)
		if err != nil {
			fmt.Printf("FAIL: Failed to reach Portainer tunnel server at %s: %v\n", params.tunnelAddr, err)
			allPassed = false
		} else {
			resp, err := httpClient.DoConnectivityCheck(req)
			if !isTunnelProbeReachable(err) {
				fmt.Printf("FAIL: Failed to reach Portainer tunnel server at %s: %v\n", params.tunnelAddr, err)
				allPassed = false
			} else {
				if resp != nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
				fmt.Printf("PASS: Portainer tunnel server is reachable at %s\n", params.tunnelAddr)
			}
		}
	}

	// print the verdict last so an interrupted or truncated stream still ends on the result
	if allPassed {
		fmt.Printf("RESULT: all %d connectivity checks passed\n", totalChecks)
	} else {
		fmt.Printf("RESULT: connectivity checks failed, see the FAIL lines above\n")
	}

	return allPassed
}

func printCheckStart(index, total int, target, addr string, timeout float64) {
	// keep the wording aligned with the PASS/FAIL lines below ("reach"/"reachable"),
	// so the announcement and its outcome read as one sentence
	fmt.Printf("CHECK (%d/%d): trying to reach the %s at %s, waiting up to %ds...\n", index, total, target, addr, int(timeout))
}

// isTunnelProbeReachable reports whether the tunnel port answered the probe.
// A nil error or a "transport connection broken" error (an unparseable reply,
// e.g. from a chisel tunnel) means it answered; any other error means it did not.
func isTunnelProbeReachable(err error) bool {
	if err == nil {
		return true
	}

	return strings.Contains(err.Error(), "transport connection broken")
}
