package docker

import (
	"context"

	"bitbucket.org/portainer/agent"
	"github.com/docker/docker/client"
)

type InfoService struct{}

func (service *InfoService) GetInformationFromDockerEngine(info map[string]string) error {

	// TODO: URL should probably be a parameter, what API version should be used?
	cli, err := client.NewClient("unix:///var/run/docker.sock", "1.30", nil, nil)
	if err != nil {
		return err
	}

	dockerInfo, err := cli.Info(context.Background())
	if err != nil {
		return err
	}

	info[agent.MemberTagKeyNodeName] = dockerInfo.Name
	info[agent.MemberTagKeyNodeAddress] = dockerInfo.Swarm.NodeAddr
	info[agent.MemberTagKeyNodeRole] = "worker"
	if dockerInfo.Swarm.ControlAvailable {
		info[agent.MemberTagKeyNodeRole] = "manager"
	}

	return nil
}
