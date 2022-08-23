package healthcheck

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/edge"
	"github.com/portainer/client-api/client"
	"github.com/portainer/client-api/client/status"
)

func Run(options *agent.Options, clusterService agent.ClusterService) error {
	if !options.EdgeMode {

		// Healthcheck not considered for regular agent in the scope of the agent auto-upgrade POC
		// We might want to consider having an healthcheck for the regular agent if that is needed/valuable
		log.Println("[INFO] [healthcheck] [message: No pre-flight checks available for regular agent deployment. Exiting.]")
		return nil
	}

	edgeKey, err := edge.RetrieveEdgeKey(options.EdgeKey, clusterService, options.DataPath)
	if err != nil {
		return errors.WithMessage(err, "Unable to retrieve Edge key")
	}

	if edgeKey == "" {
		return errors.New("Health-check for manual edge key is not supported")
	}

	decodedKey, err := edge.ParseEdgeKey(edgeKey)
	if decodedKey == nil || err != nil {
		return errors.WithMessage(err, "Failed decoding key")
	}

	parsedUrl, err := checkUrl(decodedKey.PortainerInstanceURL)
	if err != nil {
		return err
	}

	err = checkPolling(parsedUrl)
	if err != nil {
		return err
	}

	// We then check that the agent can establish a TCP connection to the Portainer instance tunnel server
	err = checkTunnel(decodedKey.TunnelServerAddr)
	if err != nil {
		return err
	}

	return nil
}

func checkUrl(keyUrl string) (*url.URL, error) {
	parsedUrl, err := url.Parse(keyUrl)
	if err != nil {
		return nil, errors.WithMessage(err, "Unable to parse Portainer URL from Edge key")
	}

	// We do a DNS resolution of the hostname associated to the Portainer instance
	// to make sure that the agent can reach out to it
	host, _, _ := net.SplitHostPort(parsedUrl.Host)

	_, err = net.LookupIP(host)
	if err != nil {
		return nil, errors.WithMessage(err, "Unable to resolve Portainer instance host")
	}

	return parsedUrl, nil
}

func checkPolling(parsedUrl *url.URL) error {
	cli := client.NewHTTPClientWithConfig(nil, client.DefaultTransportConfig().WithHost(parsedUrl.Host).WithSchemes([]string{parsedUrl.Scheme}))

	statusParams := status.NewStatusInspectParams()
	statusParams.WithContext(context.Background())
	statusParams.WithTimeout(3 * time.Second)

	if parsedUrl.Scheme == "https" {
		customTransport := http.DefaultTransport.(*http.Transport).Clone()
		customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		httpCli := &http.Client{Transport: customTransport}
		statusParams.WithHTTPClient(httpCli)
	}

	_, err := cli.Status.StatusInspect(statusParams)
	if err != nil {
		return errors.WithMessage(err, "Unable to retrieve Portainer instance status through HTTP API")
	}

	return nil
}

func checkTunnel(tunnelServerAddress string) error {
	_, err := net.Dial("tcp", tunnelServerAddress)
	if err != nil {
		return errors.WithMessage(err, "Unable to dial to Portainer instance tunnel server")
	}

	return nil
}
