package docker

import (
	"context"

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
		info[agent.ApplicationTagMode] = "standalone"
	} else {
		info[agent.ApplicationTagMode] = "swarm"
		info[agent.MemberTagKeyNodeRole] = agent.NodeRoleWorker
		if dockerInfo.Swarm.ControlAvailable {
			info[agent.MemberTagKeyNodeRole] = agent.NodeRoleManager
		}
	}

	return info, nil
}

func (service *InfoService) GetContainerIpFromDockerEngine(hostname string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return "", err
	}
	defer cli.Close()

    containerInspect, err := cli.ContainerInspect(context.Background(), hostname)
    if err != nil {
        panic (err)
	}

	var containerNetworks = containerInspect.NetworkSettings.Networks

    for key := range containerNetworks {
		var advertiseAddr = containerNetworks[key].IPAddress
		if advertiseAddr != "" {
			return advertiseAddr, nil
		}
    }

	return "", agent.ErrRetrievingAdvertiseAddr	
}