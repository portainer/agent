package serf

import (
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/logutils"
	"github.com/hashicorp/serf/serf"
	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/net"
	"github.com/rs/zerolog/log"
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
	probeTimeout         time.Duration
	probeInterval        time.Duration
	advertiseAddr        string
	clusterAddr          string
}

type ClusterServiceOptions struct {
	ProbeTimeout  time.Duration
	ProbeInterval time.Duration
	AdvertiseAddr string
	ClusterAddr   string
}

// NewClusterService returns a pointer to a ClusterService.
func NewClusterService(runtimeConfiguration *agent.RuntimeConfiguration, options ClusterServiceOptions) *ClusterService {
	return &ClusterService{
		runtimeConfiguration: runtimeConfiguration,
		probeTimeout:         options.ProbeTimeout,
		probeInterval:        options.ProbeInterval,
		advertiseAddr:        options.AdvertiseAddr,
		clusterAddr:          options.ClusterAddr,
	}
}

// Leave leaves the cluster.
func (service *ClusterService) Leave() {
	if service.cluster != nil {
		service.cluster.Leave()
	}
}

// Create will create the agent configuration and automatically join the cluster.
func (service *ClusterService) Create() error {
	// TODO: Workaround. looks like the Docker DNS cannot find any info on tasks.<service_name>
	// sometimes... Waiting a bit before starting the discovery (at least 3 seconds) seems to solve the problem.
	time.Sleep(3 * time.Second)

	log.Debug().
		Str("cluster_address", service.clusterAddr).
		Str("advertise_address", service.advertiseAddr).
		Msg("creating cluster")

	joinAddr, err := net.LookupIPAddresses(service.clusterAddr)
	if err != nil {
		log.Err(err).
			Str("host", service.clusterAddr).
			Msg("unable to retrieve a list of IP associated to the host")
		return errors.WithMessage(err, "unable to retrieve a list of IP associated to the host")
	}

	log.Debug().
		Str("cluster_address", service.clusterAddr).
		Str("advertise_address", service.advertiseAddr).
		Strs("join_address", joinAddr).
		Msg("found IP addresses for cluster address")

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
	conf.MemberlistConfig.AdvertiseAddr = service.advertiseAddr

	// These parameters should only be overridden if experiencing agent cluster instability
	// Default memberlist values should work in most clustering use cases but some
	// cluster/network topologies might cause the agent cluster to be unstable and
	// seeing a lot of agent join/leave cluster events.
	// There is no recommended value/range to be set here and instead it is recommended
	// to experiment with different values if facing instability issues.
	conf.MemberlistConfig.ProbeTimeout = service.probeTimeout
	conf.MemberlistConfig.ProbeInterval = service.probeInterval

	// Override default Serf configuration with Swarm/overlay sane defaults
	conf.ReconnectInterval = 10 * time.Second
	conf.ReconnectTimeout = 1 * time.Minute

	cluster, err := serf.Create(conf)
	if err != nil {
		return errors.WithMessage(err, "unable to create serf cluster")
	}

	nodeCount, err := cluster.Join(joinAddr, true)
	if err != nil {
		log.Debug().Err(err).Msg("unable to join cluster")
	}

	service.cluster = cluster

	log.Debug().
		Str("cluster_address", service.clusterAddr).
		Str("advertise_address", service.advertiseAddr).
		Strs("join_address", joinAddr).
		Int("contacted_nodes", nodeCount).
		Msg("started cluster service")

	return nil
}

// Members returns the list of cluster members.
func (service *ClusterService) Members() []agent.ClusterMember {
	var clusterMembers = make([]agent.ClusterMember, 0)
	if service.cluster == nil {
		return clusterMembers
	}

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
	if service.cluster == nil {
		return nil
	}

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
