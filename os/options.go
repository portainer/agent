package os

import (
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/portainer/agent"
)

const (
	EnvKeyAgentHost             = "AGENT_HOST"
	EnvKeyAgentPort             = "AGENT_PORT"
	EnvKeyClusterAddr           = "AGENT_CLUSTER_ADDR"
	EnvKeyAgentSecret           = "AGENT_SECRET"
	EnvKeyCapHostManagement     = "CAP_HOST_MANAGEMENT"
	EnvKeyEdge                  = "EDGE"
	EnvKeyEdgeKey               = "EDGE_KEY"
	EnvKeyEdgeID                = "EDGE_ID"
	EnvKeyEdgeServerHost        = "EDGE_SERVER_HOST"
	EnvKeyEdgeServerPort        = "EDGE_SERVER_PORT"
	EnvKeyEdgeInactivityTimeout = "EDGE_INACTIVITY_TIMEOUT"
	EnvKeyEdgeInsecurePoll      = "EDGE_INSECURE_POLL"
	EnvKeyLogLevel              = "LOG_LEVEL"
	EnvKeyDockerBinaryPath      = "DOCKER_BINARY_PATH"
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
		HostManagementEnabled: true,
		SharedSecret:          os.Getenv(EnvKeyAgentSecret),
		EdgeID:                os.Getenv(EnvKeyEdgeID),
		EdgeServerAddr:        agent.DefaultEdgeServerAddr,
		EdgeServerPort:        agent.DefaultEdgeServerPort,
		EdgeInactivityTimeout: agent.DefaultEdgeSleepInterval,
		EdgeInsecurePoll:      false,
		LogLevel:              agent.DefaultLogLevel,
	}

	if os.Getenv(EnvKeyCapHostManagement) != "" {
		log.Println("[WARN] [os,options] [message: the CAP_HOST_MANAGEMENT environment variable is deprecated and will likely be removed in a future version of Portainer agent]")
	}

	if os.Getenv(EnvKeyEdge) == "1" {
		options.EdgeMode = true
	}

	if os.Getenv(EnvKeyEdgeInsecurePoll) == "1" {
		options.EdgeInsecurePoll = true
	}

	if options.EdgeMode && options.EdgeID == "" {
		return nil, errors.New("missing mandatory " + EnvKeyEdgeID + " environment variable")
	}

	agentAddrEnv := os.Getenv(EnvKeyAgentHost)
	if agentAddrEnv != "" {
		options.AgentServerAddr = agentAddrEnv
	}

	agentPortEnv := os.Getenv(EnvKeyAgentPort)
	if agentPortEnv != "" {
		_, err := strconv.Atoi(agentPortEnv)
		if err != nil {
			return nil, errors.New("invalid port format in " + EnvKeyAgentPort + " environment variable")
		}
		options.AgentServerPort = agentPortEnv
	}

	edgeAddrEnv := os.Getenv(EnvKeyEdgeServerHost)
	if edgeAddrEnv != "" {
		options.EdgeServerAddr = edgeAddrEnv
	}

	edgePortEnv := os.Getenv(EnvKeyEdgeServerPort)
	if edgePortEnv != "" {
		_, err := strconv.Atoi(edgePortEnv)
		if err != nil {
			return nil, errors.New("invalid port format in " + EnvKeyEdgeServerPort + " environment variable")
		}
		options.EdgeServerPort = edgePortEnv
	}

	edgeKeyEnv := os.Getenv(EnvKeyEdgeKey)
	if edgeKeyEnv != "" {
		options.EdgeKey = edgeKeyEnv
	}

	edgeSleepIntervalEnv := os.Getenv(EnvKeyEdgeInactivityTimeout)
	if edgeSleepIntervalEnv != "" {
		_, err := time.ParseDuration(edgeSleepIntervalEnv)
		if err != nil {
			return nil, errors.New("invalid time duration format in " + EnvKeyEdgeInactivityTimeout + " environment variable")
		}
		options.EdgeInactivityTimeout = edgeSleepIntervalEnv
	}

	logLevelEnv := os.Getenv(EnvKeyLogLevel)
	if logLevelEnv != "" {
		options.LogLevel = logLevelEnv
	}

	return options, nil
}
