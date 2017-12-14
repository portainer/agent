package agent

type (
	ClusterMember struct {
		Name      string
		IPAddress string
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
)
