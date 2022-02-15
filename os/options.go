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
	EnvKeyEdgeAsync             = "EDGE_ASYNC"
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
	fAgentServerAddr       = kingpin.Flag("AgentServerAddr", EnvKeyAgentHost+" address on which the agent API will be exposed").Envar(EnvKeyAgentHost).Default(agent.DefaultAgentAddr).IP()
	fAgentServerPort       = kingpin.Flag("AgentServerPort", EnvKeyAgentPort+" port on which the agent API will be exposed").Envar(EnvKeyAgentPort).Default(agent.DefaultAgentPort).Int()
	fAgentSecurityShutdown = kingpin.Flag("AgentSecurityShutdown", EnvKeyAgentSecurityShutdown+" the duration after which the agent will be shutdown if not associated or secured by AGENT_SECRET. (defaults to 72h)").Envar(EnvKeyAgentSecurityShutdown).Default(agent.DefaultAgentSecurityShutdown).Duration()
	fClusterAddress        = kingpin.Flag("ClusterAddress", EnvKeyClusterAddr+" address (in the IP:PORT format) of an existing agent to join the agent cluster. When deploying the agent as a Docker Swarm service, we can leverage the internal Docker DNS to automatically join existing agents or form a cluster by using tasks.<AGENT_SERVICE_NAME>:<AGENT_PORT> as the address").Envar(EnvKeyClusterAddr).String()
	fSharedSecret          = kingpin.Flag("SharedSecret", EnvKeyAgentSecret+" shared secret used in the signature verification process").Envar(EnvKeyAgentSecret).String()
	fLogLevel              = kingpin.Flag("LogLevel", EnvKeyLogLevel+" defines the log output verbosity (default to INFO)").Envar(EnvKeyLogLevel).Default(agent.DefaultLogLevel).Enum("ERROR", "WARN", "INFO", "DEBUG")

	// Edge mode
	fEdgeMode = kingpin.Flag("EdgeMode", EnvKeyEdge+" enable Edge mode. Disabled by default, set to 1 or true to enable it").Envar(EnvKeyEdge).Bool()

	// TODO: yup, I would make the `--EdgeMode=async`, but not today
	fEdgeAsyncMode = kingpin.Flag("EdgeAsyncMode", EnvKeyEdgeAsync+" enable Edge Async mode. Disabled by default, set to 1 or true to enable it").Envar(EnvKeyEdgeAsync).Bool()

	fEdgeKey               = kingpin.Flag("EdgeKey", EnvKeyEdgeKey+" specify an Edge key to use at startup").Envar(EnvKeyEdgeKey).String()
	fEdgeID                = kingpin.Flag("EdgeID", EnvKeyEdgeID+" a unique identifier associated to this agent cluster").Envar(EnvKeyEdgeID).String()
	fEdgeServerAddr        = kingpin.Flag("EdgeServerAddr", EnvKeyEdgeServerHost+" address on which the Edge UI will be exposed (default to 0.0.0.0)").Envar(EnvKeyEdgeServerHost).Default(agent.DefaultEdgeServerAddr).IP()
	fEdgeServerPort        = kingpin.Flag("EdgeServerPort", EnvKeyEdgeServerPort+" port on which the Edge UI will be exposed (default to 80)").Envar(EnvKeyEdgeServerPort).Default(agent.DefaultEdgeServerPort).Int()
	fEdgeInactivityTimeout = kingpin.Flag("EdgeInactivityTimeout", EnvKeyEdgeInactivityTimeout+" timeout used by the agent to close the reverse tunnel after inactivity (default to 5m)").Envar(EnvKeyEdgeInactivityTimeout).Default(agent.DefaultEdgeSleepInterval).String()
	fEdgeInsecurePoll      = kingpin.Flag("EdgeInsecurePoll", EnvKeyEdgeInsecurePoll+" enable this option if you need the agent to poll a HTTPS Portainer instance with self-signed certificates. Disabled by default, set to 1 to enable it").Envar(EnvKeyEdgeInsecurePoll).Bool()

	// mTLS edge agent certs
	fSSLCert   = kingpin.Flag("sslcert", "Path to the SSL certificate used to identify the agent to Portainer").String()
	fSSLKey    = kingpin.Flag("sslkey", "Path to the SSL key used to identify the agent to Portainer").String()
	fSSLCacert = kingpin.Flag("sslcacert", "Path to the SSL CA certificate used to validate the Portainer server").String()
)

func (parser *EnvOptionParser) Options() (*agent.Options, error) {
	kingpin.Parse()
	return &agent.Options{
		AgentServerAddr:       fAgentServerAddr.String(),
		AgentServerPort:       strconv.Itoa(*fAgentServerPort),
		AgentSecurityShutdown: *fAgentSecurityShutdown,
		ClusterAddress:        *fClusterAddress,
		SharedSecret:          *fSharedSecret,
		EdgeMode:              *fEdgeMode,
		EdgeAsyncMode:         *fEdgeAsyncMode,
		EdgeKey:               *fEdgeKey,
		EdgeID:                *fEdgeID,
		EdgeServerAddr:        fEdgeServerAddr.String(), // TODO: really, an agent can't be both edge and non-edge, so we don't need both AgentServerAddr and EdgeServerAddr ?
		EdgeServerPort:        strconv.Itoa(*fEdgeServerPort),
		EdgeInactivityTimeout: *fEdgeInactivityTimeout,
		EdgeInsecurePoll:      *fEdgeInsecurePoll,
		LogLevel:              *fLogLevel,
		SSLCert:               *fSSLCert,
		SSLKey:                *fSSLKey,
		SSLCacert:             *fSSLCacert,
	}, nil
}
