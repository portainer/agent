package agent

type (
	ClusterMember struct {
		// TODO: container hostname for the agent is probably not needed
		Name        string
		IPAddress   string
		Port        string
		NodeName    string
		NodeAddress string
		NodeRole    string
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
		GetInformationFromDockerEngine(info map[string]string) error
	}
)

const (
	HTTPOperationHeaderName  = "X-PortainerAgent-Operation"
	HTTPOperationHeaderValue = "local"
	HTTPTargetHeaderName     = "X-PortainerAgent-Target"
	ResponseMetadataKey      = "Portainer"
	MemberTagKeyAgentPort    = "AgentPort"
	MemberTagKeyNodeAddress  = "NodeAddress"
	MemberTagKeyNodeRole     = "NodeRole"
	MemberTagKeyNodeName     = "NodeName"
)
