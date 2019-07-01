package docker

import (
	"context"
	"errors"
	"log"

	"github.com/docker/docker/client"
	"github.com/portainer/agent"
)

// InfoService is a service used to retrieve information from a Docker environment.
type InfoService struct{}

// GetInformationFromDockerEngine retrieves information from a Docker environment
// and returns a map of labels.
func (service *InfoService) GetInformationFromDockerEngine() (map[string]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	dockerInfo, err := cli.Info(context.Background())
	if err != nil {
		return nil, err
	}

	info := make(map[string]string)
	info[agent.MemberTagKeyNodeName] = dockerInfo.Name

	if dockerInfo.Swarm.NodeID == "" {
		info[agent.MemberTagEngineStatus] = "standalone"
	} else {
		info[agent.MemberTagEngineStatus] = "swarm"
		info[agent.MemberTagKeyNodeRole] = agent.NodeRoleWorker
		if dockerInfo.Swarm.ControlAvailable {
			info[agent.MemberTagKeyNodeRole] = agent.NodeRoleManager
		}
	}

	return info, nil
}

// TODO: have to be careful with this implementation. If the container is not using host mode port
// when publishing ports it will be automatically added to the ingress network.
// This implementation might return the IP address used in the ingress network and cause network issues
// when trying to form the cluster.
// Must be documented.
func (service *InfoService) GetContainerIpFromDockerEngine(containerName string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containerInspect, err := cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		return "", err
	}

	log.Printf("[DEBUG] [docker] [network_count: %d] [Selecting IP address from container networks]", len(containerInspect.NetworkSettings.Networks))

	for _, network := range containerInspect.NetworkSettings.Networks {
		if network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}

	return "", errors.New("unable to retrieve the address on which the agent can advertise. Check your network settings")
}
