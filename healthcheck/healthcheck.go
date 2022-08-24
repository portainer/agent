package healthcheck

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/edge"
	"github.com/portainer/agent/edge/client"
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
		log.Println("[INFO] [healthcheck] [message: No pre-flight checks available when edge key is manually entered. Exiting.]")
		return nil
	}

	decodedKey, err := edge.ParseEdgeKey(edgeKey)
	if decodedKey == nil || err != nil {
		return errors.WithMessage(err, "Failed decoding key")
	}

	_, err = checkUrl(decodedKey.PortainerInstanceURL)
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] [healthcheck] [message: Url reachable]")

	err = checkPolling(decodedKey.PortainerInstanceURL, options)
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] [healthcheck] [message: Portainer status check passed]")

	// We then check that the agent can establish a TCP connection to the Portainer instance tunnel server
	err = checkTunnel(decodedKey.TunnelServerAddr)
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] [healthcheck] [message: Agent can open TCP connection to Portainer]")

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

func checkPolling(portainerUrl string, options *agent.Options) error {
	statusUrl := fmt.Sprintf("%s/api/status", portainerUrl)

	req, err := http.NewRequest(http.MethodGet, statusUrl, nil)
	if err != nil {
		return errors.WithMessage(err, "Failed creating request")
	}

	cli := client.BuildHTTPClient(10, options)

	resp, err := cli.Do(req)
	if err != nil {
		return errors.WithMessage(err, "Failed sending request")
	}
	defer resp.Body.Close()

	return nil
}

func checkTunnel(tunnelServerAddress string) error {
	_, err := net.Dial("tcp", tunnelServerAddress)
	if err != nil {
		return errors.WithMessage(err, "Unable to dial to Portainer instance tunnel server")
	}

	return nil
}
