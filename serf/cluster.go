package serf

import (
	"log"
	"os"

	"bitbucket.org/portainer/agent"
	"github.com/hashicorp/logutils"
	"github.com/hashicorp/serf/serf"
)

type ClusterService struct {
	cluster *serf.Serf
}

func NewClusterService() *ClusterService {
	return &ClusterService{}
}

func (service *ClusterService) Leave() {
	if service.cluster != nil {
		service.cluster.Leave()
	}
}

func (service *ClusterService) Create(advertiseAddr, joinAddr string, tags map[string]string) error {

	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR"},
		MinLevel: logutils.LogLevel("INFO"),
		Writer:   os.Stderr,
	}

	conf := serf.DefaultConfig()
	conf.Init()
	conf.Tags = tags
	conf.MemberlistConfig.LogOutput = filter
	conf.LogOutput = filter
	conf.MemberlistConfig.AdvertiseAddr = advertiseAddr
	log.Printf("[DEBUG] - Serf configured with AdvertiseAddr: %s\n", advertiseAddr)

	cluster, err := serf.Create(conf)
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] - Will join cluster via: %s\n", joinAddr)

	_, err = cluster.Join([]string{joinAddr}, true)
	if err != nil {
		log.Printf("[DEBUG] - Couldn't join cluster, starting own: %v\n", err)
	}

	service.cluster = cluster

	return nil
}

func (service *ClusterService) Members() []agent.ClusterMember {
	var clusterMembers = make([]agent.ClusterMember, 0)

	members := service.cluster.Members()

	for _, member := range members {
		if member.Status == serf.StatusAlive {
			clusterMember := agent.ClusterMember{
				IPAddress: member.Addr.String(),
				Port:      member.Tags[agent.MemberTagKeyAgentPort],
				NodeRole:  member.Tags[agent.MemberTagKeyNodeRole],
				NodeName:  member.Tags[agent.MemberTagKeyNodeName],
			}
			clusterMembers = append(clusterMembers, clusterMember)
		}
	}

	return clusterMembers
}

func (service *ClusterService) GetMemberByRole(role string) *agent.ClusterMember {
	members := service.Members()
	for _, member := range members {
		if member.NodeRole == role {
			return &member
		}
	}

	return nil
}

func (service *ClusterService) GetMemberByNodeName(nodeName string) *agent.ClusterMember {
	members := service.Members()
	for _, member := range members {
		if member.NodeName == nodeName {
			return &member
		}
	}

	return nil
}
