package agent

type (
	ClusterMember struct {
		IPAddress string
		Port      string
		NodeName  string
		NodeRole  string
	}

	AgentMetadata struct {
		Agent struct {
			NodeName string `json:"NodeName"`
		} `json:"Agent"`
	}

	ClusterService interface {
		Create(advertiseAddr, joinAddr string, tags map[string]string) error
		Members() []ClusterMember
		Leave()
		GetMemberByRole(role string) *ClusterMember
		GetMemberByNodeName(nodeName string) *ClusterMember
	}

	InfoService interface {
		GetInformationFromDockerEngine() (map[string]string, error)
	}
)

const (
	DefaultListenAddr         = "0.0.0.0"
	DefaultAgentPort          = "9001"
	DefaultLogLevel           = "INFO"
	SupportedDockerAPIVersion = "1.24"
	HTTPTargetHeaderName      = "X-PortainerAgent-Target"
	ResponseMetadataKey       = "Portainer"
	MemberTagKeyAgentPort     = "AgentPort"
	MemberTagKeyNodeName      = "NodeName"
	MemberTagKeyNodeRole      = "NodeRole"
	NodeRoleManager           = "manager"
	NodeRoleWorker            = "worker"
)
