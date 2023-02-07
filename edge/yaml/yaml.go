package yaml

import (
	"github.com/portainer/agent"
)

type yaml struct {
	fileContent         string
	registryCredentials []agent.RegistryCredentials
}

func NewYAML(fileContent string, credentials []agent.RegistryCredentials) yaml {
	return yaml{
		fileContent:         fileContent,
		registryCredentials: credentials,
	}
}
