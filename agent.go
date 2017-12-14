package agent

type (
	ClusterMember struct {
		Name      string
		IPAddress string
	}

	AgentMetadata struct {
		Agent struct {
			Node string `json:"Node"`
		} `json:"Agent"`
	}

	ClusterService interface {
		Create(advertiseAddr, joinAddr string) error
		Members() ([]ClusterMember, error)
		Leave()
	}
)

const (
	HTTPOperationHeaderName  = "X-PortainerAgent-Operation"
	HTTPOperationHeaderValue = "local"
	HTTPTargetHeaderName     = "X-PortainerAgent-Target"
	ResponseMetadataKey      = "Portainer"
)
