package os

import (
	"errors"
	"os"
	"strconv"

	"github.com/portainer/agent"
)

const (
	EnvKeyAgentAddr         = "AGENT_ADDR"
	EnvKeyAgentPort         = "AGENT_PORT"
	EnvKeyClusterAddr       = "AGENT_CLUSTER_ADDR"
	EnvKeyAgentSecret       = "AGENT_SECRET"
	EnvKeyCapHostManagement = "CAP_HOST_MANAGEMENT"
	EnvKeyEdge              = "EDGE"
	EnvKeyEdgeKey           = "EDGE_KEY"
	EnvKeyEdgeTunnelServer  = "EDGE_TUNNEL_SERVER"
	EnvKeyEdgeServerAddr    = "EDGE_SERVER_ADDR"
	EnvKeyEdgeServerPort    = "EDGE_SERVER_PORT"
	EnvKeyLogLevel          = "LOG_LEVEL"
)

type EnvOptionParser struct{}

func NewEnvOptionParser() *EnvOptionParser {
	return &EnvOptionParser{}
}

func (parser *EnvOptionParser) Options() (*agent.Options, error) {
	options := &agent.Options{
		AgentServerAddr:       agent.DefaultAgentAddr,
		AgentServerPort:       agent.DefaultAgentPort,
		ClusterAddress:        os.Getenv(EnvKeyClusterAddr),
		HostManagementEnabled: false,
		SharedSecret:          os.Getenv(EnvKeyAgentSecret),
		EdgeServerAddr:        agent.DefaultEdgeServerAddr,
		EdgeServerPort:        agent.DefaultEdgeServerPort,
		EdgeTunnelServerAddr:  os.Getenv(EnvKeyEdgeTunnelServer),
		LogLevel:              agent.DefaultLogLevel,
	}

	if os.Getenv(EnvKeyCapHostManagement) == "1" {
		options.HostManagementEnabled = true
	}

	if os.Getenv(EnvKeyEdge) == "1" {
		options.EdgeMode = true
	}

	agentAddrEnv := os.Getenv(EnvKeyAgentAddr)
	if agentAddrEnv != "" {
		options.AgentServerAddr = agentAddrEnv
	}

	agentPortEnv := os.Getenv(EnvKeyAgentPort)
	if agentPortEnv != "" {
		_, err := strconv.Atoi(agentPortEnv)
		if err != nil {
			return nil, errors.New("Invalid port format in " + EnvKeyAgentPort + " environment variable")
		}
		options.AgentServerPort = agentPortEnv
	}

	edgeAddrEnv := os.Getenv(EnvKeyEdgeServerAddr)
	if edgeAddrEnv != "" {
		options.EdgeServerAddr = edgeAddrEnv
	}

	edgePortEnv := os.Getenv(EnvKeyEdgeServerPort)
	if edgePortEnv != "" {
		_, err := strconv.Atoi(edgePortEnv)
		if err != nil {
			return nil, errors.New("Invalid port format in " + EnvKeyEdgeServerPort + " environment variable")
		}
		options.EdgeServerPort = edgePortEnv
	}

	edgeKeyEnv := os.Getenv(EnvKeyEdgeKey)
	if edgeKeyEnv != "" {
		options.EdgeKey = edgeKeyEnv
	}

	logLevelEnv := os.Getenv(EnvKeyLogLevel)
	if logLevelEnv != "" {
		options.LogLevel = logLevelEnv
	}

	return options, nil
}
