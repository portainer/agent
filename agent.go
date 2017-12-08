package agent

import "bitbucket.org/portainer/agent"

type (
	ClusterMember struct {
		Name      string
		IPAddress string
	}

	ClusterService interface {
		Create(advertiseAddr, joinAddr string) error
		Members() ([]agent.ClusterMember, error)
		Leave()
	}
)
