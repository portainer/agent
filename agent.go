package agent

type (
	ClusterMember struct {
		Name        string
		IPAddress   string
		AgentPort   string
		NodeAddress string
		NodeRole    string
	}

	AgentMetadata struct {
		Agent struct {
			Node string `json:"Node"`
		} `json:"Agent"`
	}

	ClusterService interface {
		Create(advertiseAddr, joinAddr string, tags map[string]string) error
		Members() []ClusterMember
		Leave()
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
)
