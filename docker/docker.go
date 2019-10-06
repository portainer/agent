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

// GetContainerIpFromDockerEngine is used to retrieve the IP address of the container through Docker.
// It will inspect the container to retrieve the networks associated to the container and returns the first IP associated
// to the first network found.
// This might cause some problem if the agent is part of the ingress network.
func (service *InfoService) GetContainerIpFromDockerEngine(containerName string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containerList, err := cli.ContainerList(context.Background(), types.ContainerListOptions{All: true})
	if err != nil {
		return "", err
	}

	log.Println("[DEBUG] [docker] [Print list]")
	for _, container := range containerList {
		log.Printf("[DEBUG] [docker] [container_name: %s]", container.Names[0])
	}

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

		if networkInspect.Ingress || networkInspect.Scope != "swarm" {
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
