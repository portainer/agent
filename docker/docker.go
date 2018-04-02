package docker

import (
	"context"

	"bitbucket.org/portainer/agent"
	"github.com/docker/docker/client"
)

type InfoService struct{}

func (service *InfoService) GetInformationFromDockerEngine() (map[string]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(agent.SupportedDockerAPIVersion))
	if err != nil {
		return nil, err
	}

	dockerInfo, err := cli.Info(context.Background())
	if err != nil {
		return nil, err
	}

	info := make(map[string]string)
	info[agent.MemberTagKeyNodeName] = dockerInfo.Name
	info[agent.MemberTagKeyNodeRole] = agent.NodeRoleWorker
	if dockerInfo.Swarm.ControlAvailable {
		info[agent.MemberTagKeyNodeRole] = agent.NodeRoleManager
	}

	return info, nil
}
