package docker

import (
	"context"
	"errors"
	"time"

	"github.com/portainer/agent"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
)

const (
	serviceNameLabel = "com.docker.swarm.service.name"
	clientTimeout    = 1 * time.Minute
)

// DockerInfoService is a service used to retrieve information from a Docker environment
// using the Docker library.
type InfoService struct{}

// NewInfoService returns a pointer to an instance of DockerInfoService
func NewInfoService() *InfoService {
	return &InfoService{}
}

// GetRuntimeConfigurationFromDockerEngine retrieves information from a Docker environment
// and returns a map of labels.
func (service *InfoService) GetRuntimeConfigurationFromDockerEngine() (*agent.RuntimeConfig, error) {
	cli, err := NewClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	dockerInfo, err := cli.Info(context.Background())
	if err != nil {
		return nil, err
	}

	runtimeConfig := &agent.RuntimeConfig{
		NodeName:     dockerInfo.Name,
		DockerConfig: agent.DockerRuntimeConfig{},
	}

	if dockerInfo.Swarm.NodeID == "" {
		getStandaloneConfig(runtimeConfig)
	} else {

		err := getSwarmConfig(runtimeConfig, dockerInfo, cli)
		if err != nil {
			return nil, err
		}

	}

	return runtimeConfig, nil
}

// GetContainerIpFromDockerEngine is used to retrieve the IP address of the container through Docker.
// It will inspect the container to retrieve the networks associated to the container and returns the IP associated
// to the first network found that is not an ingress network. If the ignoreNonSwarmNetworks parameter is specified,
// it will also ignore non Swarm scoped networks.
func (service *InfoService) GetContainerIpFromDockerEngine(containerName string, ignoreNonSwarmNetworks bool) (string, error) {
	cli, err := NewClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containerInspect, err := cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		return "", err
	}

	networks, err := fetchNetworkInfo(cli, containerInspect.NetworkSettings.Networks)
	if err != nil {
		return "", err
	}

	overlayCount := countOverlays(networks)

	if overlayCount > 1 {
		log.Warn().
			Int("network_count", len(containerInspect.NetworkSettings.Networks)).
			Msg("Agent container running in more than one overlay network. This might cause communication issues")
	}

	for _, network := range networks {
		if network.resource.Ingress || (ignoreNonSwarmNetworks && network.resource.Scope != "swarm") {
			log.Debug().
				Str("network_name", network.resource.Name).
				Str("scope", network.resource.Scope).
				Bool("ingress", network.resource.Ingress).
				Msg("skipping invalid container network")

			continue
		}

		if network.settings.IPAddress != "" {
			log.Debug().
				Str("ip_address", network.settings.IPAddress).
				Str("network_name", network.name).
				Msg("retrieving IP address from container network")

			return network.settings.IPAddress, nil
		}
	}

	return "", errors.New("unable to retrieve the address on which the agent can advertise. Check your network settings")
}

// GetServiceNameFromDockerEngine is used to return the name of the Swarm service the agent is part of.
// The service name is retrieved through container labels.
func (service *InfoService) GetServiceNameFromDockerEngine(containerName string) (string, error) {
	cli, err := NewClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containerInspect, err := cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		return "", err
	}

	return containerInspect.Config.Labels[serviceNameLabel], nil
}

func getStandaloneConfig(config *agent.RuntimeConfig) {
	config.DockerConfig.EngineType = agent.EngineTypeStandalone
}

func getSwarmConfig(config *agent.RuntimeConfig, dockerInfo system.Info, cli *client.Client) error {
	config.DockerConfig.EngineType = agent.EngineTypeSwarm
	config.DockerConfig.NodeRole = agent.NodeRoleWorker

	if dockerInfo.Swarm.ControlAvailable {
		config.DockerConfig.NodeRole = agent.NodeRoleManager

		node, _, err := cli.NodeInspectWithRaw(context.Background(), dockerInfo.Swarm.NodeID)
		if err != nil {
			return err
		}

		if node.ManagerStatus.Leader {
			config.DockerConfig.Leader = true
		}
	}

	return nil
}

func NewClient() (*client.Client, error) {
	return client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
		client.WithTimeout(clientTimeout),
	)
}

func withCli(callback func(cli *client.Client) error) error {
	cli, err := NewClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	return callback(cli)
}

type networkInfo struct {
	resource types.NetworkResource
	settings *network.EndpointSettings
	name     string
}

func fetchNetworkInfo(cli *client.Client, networkSettings map[string]*network.EndpointSettings) ([]networkInfo, error) {
	networks := []networkInfo{}

	for networkName, network := range networkSettings {
		networkInspect, err := cli.NetworkInspect(context.Background(), network.NetworkID, types.NetworkInspectOptions{})
		if err != nil {
			return nil, err
		}

		networks = append(networks, networkInfo{
			resource: networkInspect,
			settings: network,
			name:     networkName,
		})

	}

	return networks, nil
}

func countOverlays(networks []networkInfo) int {
	overlayCount := 0

	for _, network := range networks {
		if network.resource.Driver == "overlay" && !network.resource.Ingress {
			log.Debug().
				Str("network_name", network.name).
				Msg("found overlay network")

			overlayCount++
		}
	}

	return overlayCount
}
