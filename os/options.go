package os

import (
	"strconv"

	"github.com/portainer/agent"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	EnvKeyAgentHost             = "AGENT_HOST"
	EnvKeyAgentPort             = "AGENT_PORT"
	EnvKeyClusterAddr           = "AGENT_CLUSTER_ADDR"
	EnvKeyAgentSecret           = "AGENT_SECRET"
	EnvKeyAgentSecurityShutdown = "AGENT_SECRET_TIMEOUT"
	//EnvKeyCapHostManagement     = "CAP_HOST_MANAGEMENT"  // deprecated and unused
	EnvKeyEdge                  = "EDGE"
	EnvKeyEdgeKey               = "EDGE_KEY"
	EnvKeyEdgeID                = "EDGE_ID"
	EnvKeyEdgeServerHost        = "EDGE_SERVER_HOST"
	EnvKeyEdgeServerPort        = "EDGE_SERVER_PORT"
	EnvKeyEdgeInactivityTimeout = "EDGE_INACTIVITY_TIMEOUT"
	EnvKeyEdgeInsecurePoll      = "EDGE_INSECURE_POLL"
	EnvKeyLogLevel              = "LOG_LEVEL"
	//EnvKeyDockerBinaryPath      = "DOCKER_BINARY_PATH" //unused
)

type EnvOptionParser struct{}

func NewEnvOptionParser() *EnvOptionParser {
	return &EnvOptionParser{}
}

var (
	fAgentServerAddr       = kingpin.Flag("AgentServerAddr", "").Envar(EnvKeyAgentHost).Default(agent.DefaultAgentAddr).IP()
	fAgentServerPort       = kingpin.Flag("AgentServerPort", "").Envar(EnvKeyAgentPort).Default(agent.DefaultAgentPort).Int()
	fAgentSecurityShutdown = kingpin.Flag("AgentSecurityShutdown", "").Envar(EnvKeyAgentSecurityShutdown).Default(agent.DefaultAgentSecurityShutdown).Duration()
	fClusterAddress        = kingpin.Flag("ClusterAddress", "").Envar(EnvKeyClusterAddr).String()
	fSharedSecret          = kingpin.Flag("SharedSecret", "").Envar(EnvKeyAgentSecret).String()
	fLogLevel              = kingpin.Flag("LogLevel", "").Envar(EnvKeyLogLevel).Default(agent.DefaultLogLevel).Enum("INFO", "")

	// Edge mode
	fEdgeMode              = kingpin.Flag("EdgeMode", "").Envar(EnvKeyEdge).Bool()
	fEdgeKey               = kingpin.Flag("EdgeKey", "").Envar(EnvKeyEdgeKey).String()
	fEdgeID                = kingpin.Flag("EdgeID", "").Envar(EnvKeyEdgeID).String()
	fEdgeServerAddr        = kingpin.Flag("EdgeServerAddr", "").Envar(EnvKeyEdgeServerHost).Default(agent.DefaultEdgeServerAddr).IP()
	fEdgeServerPort        = kingpin.Flag("EdgeServerPort", "").Envar(EnvKeyEdgeServerPort).Default(agent.DefaultEdgeServerPort).Int()
	fEdgeInactivityTimeout = kingpin.Flag("EdgeInactivityTimeout", "").Envar(EnvKeyEdgeInactivityTimeout).Default(agent.DefaultEdgeSleepInterval).String()
	fEdgeInsecurePoll      = kingpin.Flag("EdgeInsecurePoll", "").Envar(EnvKeyEdgeInsecurePoll).Bool()
)

func (parser *EnvOptionParser) Options() (*agent.Options, error) {
	kingpin.Parse()
	return &agent.Options{
		AgentServerAddr:       fAgentServerAddr.String(),
		AgentServerPort:       strconv.Itoa(*fAgentServerPort),
		AgentSecurityShutdown: *fAgentSecurityShutdown,
		ClusterAddress:        *fClusterAddress,
		HostManagementEnabled: true, // TODO: is this a constant? can we get rid of it?
		SharedSecret:          *fSharedSecret,
		EdgeMode:              *fEdgeMode,
		EdgeKey:               *fEdgeKey,
		EdgeID:                *fEdgeID,
		EdgeServerAddr:        fEdgeServerAddr.String(), // TODO: really, an agent can't be both edge and non-edge, so we don't need both AgentServerAddr and EdgeServerAddr ?
		EdgeServerPort:        strconv.Itoa(*fEdgeServerPort),
		EdgeInactivityTimeout: *fEdgeInactivityTimeout,
		EdgeInsecurePoll:      *fEdgeInsecurePoll,
		LogLevel:              *fLogLevel,
	}, nil
}
