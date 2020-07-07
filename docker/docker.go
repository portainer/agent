package docker

import (
	"context"
	"errors"
	"log"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/portainer/agent"
)

const (
	serviceNameLabel = "com.docker.swarm.service.name"
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
func (service *InfoService) GetRuntimeConfigurationFromDockerEngine() (*agent.RuntimeConfiguration, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	dockerInfo, err := cli.Info(context.Background())
	if err != nil {
		return nil, err
	}

	runtimeConfiguration := &agent.RuntimeConfiguration{
		NodeName:            dockerInfo.Name,
		DockerConfiguration: agent.DockerRuntimeConfiguration{},
	}

	if dockerInfo.Swarm.NodeID == "" {
		getStandaloneConfiguration(runtimeConfiguration)
	} else {

		err := getSwarmConfiguration(runtimeConfiguration, dockerInfo, cli)
		if err != nil {
			return nil, err
		}

	}

	return runtimeConfiguration, nil
}

// GetContainerIpFromDockerEngine is used to retrieve the IP address of the container through Docker.
// It will inspect the container to retrieve the networks associated to the container and returns the IP associated
// to the first network found that is not an ingress network. If the ignoreNonSwarmNetworks parameter is specified,
// it will also ignore non Swarm scoped networks.
func (service *InfoService) GetContainerIpFromDockerEngine(containerName string, ignoreNonSwarmNetworks bool) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containerInspect, err := cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		return "", err
	}

	if len(containerInspect.NetworkSettings.Networks) > 1 {
		log.Printf("[WARN] [docker] [network_count: %d] [message: Agent container running in more than a single Docker network. This might cause communication issues]", len(containerInspect.NetworkSettings.Networks))
	}

	for networkName, network := range containerInspect.NetworkSettings.Networks {
		networkInspect, err := cli.NetworkInspect(context.Background(), network.NetworkID, types.NetworkInspectOptions{})
		if err != nil {
			return "", err
		}

		if networkInspect.Ingress || (ignoreNonSwarmNetworks && networkInspect.Scope != "swarm") {
			log.Printf("[DEBUG] [docker] [network_name: %s] [scope: %s] [ingress: %t] [message: Skipping invalid container network]", networkInspect.Name, networkInspect.Scope, networkInspect.Ingress)
			continue
		}

		if network.IPAddress != "" {
			log.Printf("[DEBUG] [docker] [ip_address: %s] [network_name: %s] [message: Retrieving IP address from container network]", network.IPAddress, networkName)
			return network.IPAddress, nil
		}
	}

	return "", errors.New("unable to retrieve the address on which the agent can advertise. Check your network settings")
}

// GetServiceNameFromDockerEngine is used to return the name of the Swarm service the agent is part of.
// The service name is retrieved through container labels.
func (service *InfoService) GetServiceNameFromDockerEngine(containerName string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
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

func getStandaloneConfiguration(config *agent.RuntimeConfiguration) {
	config.DockerConfiguration.EngineStatus = agent.EngineStatusStandalone
}

func getSwarmConfiguration(config *agent.RuntimeConfiguration, dockerInfo types.Info, cli *client.Client) error {
	config.DockerConfiguration.EngineStatus = agent.EngineStatusSwarm
	config.DockerConfiguration.NodeRole = agent.NodeRoleWorker

	if dockerInfo.Swarm.ControlAvailable {
		config.DockerConfiguration.NodeRole = agent.NodeRoleManager

		node, _, err := cli.NodeInspectWithRaw(context.Background(), dockerInfo.Swarm.NodeID)
		if err != nil {
			return err
		}

		if node.ManagerStatus.Leader {
			config.DockerConfiguration.Leader = true
		}
	}

	return nil
}
