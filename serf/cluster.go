package serf

import (
	"log"
	"os"

	"bitbucket.org/portainer/agent"
	"github.com/hashicorp/logutils"
	"github.com/hashicorp/serf/serf"
)

// ClusterService is a service used to manage cluster related actions such as joining
// the cluster, retrieving members in the clusters...
type ClusterService struct {
	cluster *serf.Serf
}

// NewClusterService returns a pointer to a ClusterService.
func NewClusterService() *ClusterService {
	return &ClusterService{}
}

// Leave leaves the cluster.
func (service *ClusterService) Leave() {
	if service.cluster != nil {
		service.cluster.Leave()
	}
}

// Create will create the agent configuration and automatically join the cluster.
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

// Members returns the list of cluster members.
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

// GetMemberByRole will return the first member with the specified role.
func (service *ClusterService) GetMemberByRole(role string) *agent.ClusterMember {
	members := service.Members()
	for _, member := range members {
		if member.NodeRole == role {
			return &member
		}
	}

	return nil
}

// GetMemberByNodeName will return the first member with the specified node name.
func (service *ClusterService) GetMemberByNodeName(nodeName string) *agent.ClusterMember {
	members := service.Members()
	for _, member := range members {
		if member.NodeName == nodeName {
			return &member
		}
	}

	return nil
}
