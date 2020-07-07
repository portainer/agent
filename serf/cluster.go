package serf

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hashicorp/logutils"
	"github.com/hashicorp/serf/serf"
	"github.com/portainer/agent"
)

const (
	memberTagKeyAgentPort    = "AgentPort"
	memberTagKeyIsLeader     = "NodeIsLeader"
	memberTagKeyNodeName     = "NodeName"
	memberTagKeyNodeRole     = "DockerNodeRole"
	memberTagKeyEngineStatus = "DockerEngineStatus"
	memberTagKeyEdgeKeySet   = "EdgeKeySet"

	memberTagValueEngineStatusSwarm      = "swarm"
	memberTagValueEngineStatusStandalone = "standalone"
	memberTagValueNodeRoleManager        = "manager"
	memberTagValueNodeRoleWorker         = "worker"
)

// ClusterService is a service used to manage cluster related actions such as joining
// the cluster, retrieving members in the clusters...
type ClusterService struct {
	runtimeConfiguration *agent.RuntimeConfiguration
	cluster              *serf.Serf
}

// NewClusterService returns a pointer to a ClusterService.
func NewClusterService(runtimeConfiguration *agent.RuntimeConfiguration) *ClusterService {
	return &ClusterService{
		runtimeConfiguration: runtimeConfiguration,
	}
}

// Leave leaves the cluster.
func (service *ClusterService) Leave() {
	if service.cluster != nil {
		service.cluster.Leave()
	}
}

// Create will create the agent configuration and automatically join the cluster.
func (service *ClusterService) Create(advertiseAddr string, joinAddr []string) error {
	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR"},
		MinLevel: logutils.LogLevel("INFO"),
		Writer:   os.Stderr,
	}

	conf := serf.DefaultConfig()
	conf.Init()
	conf.NodeName = fmt.Sprintf("%s-%s", service.runtimeConfiguration.NodeName, conf.NodeName)
	conf.Tags = convertRuntimeConfigurationToTagMap(service.runtimeConfiguration)
	conf.MemberlistConfig.LogOutput = filter
	conf.LogOutput = filter
	conf.MemberlistConfig.AdvertiseAddr = advertiseAddr

	// Override default Serf configuration with Swarm/overlay sane defaults
	conf.ReconnectInterval = 10 * time.Second
	conf.ReconnectTimeout = 1 * time.Minute

	log.Printf("[DEBUG] [cluster,serf] [advertise_address: %s] [join_address: %s]", advertiseAddr, joinAddr)

	cluster, err := serf.Create(conf)
	if err != nil {
		return err
	}

	nodeCount, err := cluster.Join(joinAddr, true)
	if err != nil {
		log.Printf("[DEBUG] [cluster,serf] [message: Unable to join cluster] [error: %s]", err)
	}
	log.Printf("[DEBUG] [cluster,serf] [contacted_nodes: %d]", nodeCount)

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
				IPAddress:  member.Addr.String(),
				Port:       member.Tags[memberTagKeyAgentPort],
				NodeRole:   member.Tags[memberTagKeyNodeRole],
				NodeName:   member.Tags[memberTagKeyNodeName],
				EdgeKeySet: false,
			}

			_, ok := member.Tags[memberTagKeyEdgeKeySet]
			if ok {
				clusterMember.EdgeKeySet = true
			}

			clusterMembers = append(clusterMembers, clusterMember)
		}
	}

	return clusterMembers
}

// GetMemberByRole will return the first member with the specified role.
func (service *ClusterService) GetMemberByRole(role agent.DockerNodeRole) *agent.ClusterMember {
	members := service.Members()

	roleString := memberTagValueNodeRoleManager
	if role == agent.NodeRoleWorker {
		roleString = memberTagValueNodeRoleWorker
	}

	for _, member := range members {
		if member.NodeRole == roleString {
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

// GetMemberWithEdgeKeySet will return the first member with the EdgeKeySet tag set.
func (service *ClusterService) GetMemberWithEdgeKeySet() *agent.ClusterMember {
	members := service.Members()
	for _, member := range members {
		if member.EdgeKeySet {
			return &member
		}
	}

	return nil
}

// UpdateRuntimeConfiguration propagate the new runtimeConfiguration to the cluster
func (service *ClusterService) UpdateRuntimeConfiguration(runtimeConfiguration *agent.RuntimeConfiguration) error {
	service.runtimeConfiguration = runtimeConfiguration
	tagsMap := convertRuntimeConfigurationToTagMap(runtimeConfiguration)
	return service.cluster.SetTags(tagsMap)
}

// GetRuntimeConfiguration returns the runtimeConfiguration associated to the service
func (service *ClusterService) GetRuntimeConfiguration() *agent.RuntimeConfiguration {
	return service.runtimeConfiguration
}

func convertRuntimeConfigurationToTagMap(runtimeConfiguration *agent.RuntimeConfiguration) map[string]string {
	tagsMap := map[string]string{}

	if runtimeConfiguration.EdgeKeySet {
		tagsMap[memberTagKeyEdgeKeySet] = "set"
	}

	tagsMap[memberTagKeyEngineStatus] = memberTagValueEngineStatusStandalone
	if runtimeConfiguration.DockerConfiguration.EngineStatus == agent.EngineStatusSwarm {
		tagsMap[memberTagKeyEngineStatus] = memberTagValueEngineStatusSwarm
	}

	tagsMap[memberTagKeyAgentPort] = runtimeConfiguration.AgentPort

	if runtimeConfiguration.DockerConfiguration.Leader {
		tagsMap[memberTagKeyIsLeader] = "1"
	}

	tagsMap[memberTagKeyNodeName] = runtimeConfiguration.NodeName

	tagsMap[memberTagKeyNodeRole] = memberTagValueNodeRoleManager
	if runtimeConfiguration.DockerConfiguration.NodeRole == agent.NodeRoleWorker {
		tagsMap[memberTagKeyNodeRole] = memberTagValueNodeRoleWorker
	}

	return tagsMap
}
