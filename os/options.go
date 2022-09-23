package os

import (
	"strconv"

	"github.com/portainer/agent"
	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	EnvKeyAgentHost             = "AGENT_HOST"
	EnvKeyAgentPort             = "AGENT_PORT"
	EnvKeyClusterAddr           = "AGENT_CLUSTER_ADDR"
	EnvKeyClusterProbeTimeout   = "AGENT_CLUSTER_PROBE_TIMEOUT"
	EnvKeyClusterProbeInterval  = "AGENT_CLUSTER_PROBE_INTERVAL"
	EnvKeyAgentSecret           = "AGENT_SECRET"
	EnvKeyAgentSecurityShutdown = "AGENT_SECRET_TIMEOUT"
	EnvKeyAssetsPath            = "ASSETS_PATH"
	EnvKeyDataPath              = "DATA_PATH"
	EnvKeyEdge                  = "EDGE"
	EnvKeyEdgeAsync             = "EDGE_ASYNC"
	EnvKeyEdgeKey               = "EDGE_KEY"
	EnvKeyEdgeID                = "EDGE_ID"
	EnvKeyEdgeServerHost        = "EDGE_SERVER_HOST"
	EnvKeyEdgeServerPort        = "EDGE_SERVER_PORT"
	EnvKeyEdgeInactivityTimeout = "EDGE_INACTIVITY_TIMEOUT"
	EnvKeyEdgeInsecurePoll      = "EDGE_INSECURE_POLL"
	EnvKeyEdgeTunnel            = "EDGE_TUNNEL"
	EnvKeyHealthCheck           = "HEALTH_CHECK"
	EnvKeyLogLevel              = "LOG_LEVEL"
	EnvKeySSLCert               = "MTLS_SSL_CERT"
	EnvKeySSLKey                = "MTLS_SSL_KEY"
	EnvKeySSLCACert             = "MTLS_SSL_CA"
	EnvKeyCertRetryInterval     = "MTLS_CERT_RETRY_INTERVAL"
)

type EnvOptionParser struct{}

func NewEnvOptionParser() *EnvOptionParser {
	return &EnvOptionParser{}
}

var (
	fAssetsPath            = kingpin.Flag("assets", EnvKeyAssetsPath+" path to the assets folder").Envar(EnvKeyAssetsPath).Default(agent.DefaultAssetsPath).String()
	fAgentServerAddr       = kingpin.Flag("host", EnvKeyAgentHost+" address on which the agent API will be exposed").Envar(EnvKeyAgentHost).Default(agent.DefaultAgentAddr).IP()
	fAgentServerPort       = kingpin.Flag("port", EnvKeyAgentPort+" port on which the agent API will be exposed").Envar(EnvKeyAgentPort).Default(agent.DefaultAgentPort).Int()
	fAgentSecurityShutdown = kingpin.Flag("secret-timeout", EnvKeyAgentSecurityShutdown+" the duration after which the agent will be shutdown if not associated or secured by AGENT_SECRET. (defaults to 72h)").Envar(EnvKeyAgentSecurityShutdown).Default(agent.DefaultAgentSecurityShutdown).Duration()
	fClusterAddress        = kingpin.Flag("cluster-addr", EnvKeyClusterAddr+" address (in the IP:PORT format) of an existing agent to join the agent cluster. When deploying the agent as a Docker Swarm service, we can leverage the internal Docker DNS to automatically join existing agents or form a cluster by using tasks.<AGENT_SERVICE_NAME>:<AGENT_PORT> as the address").Envar(EnvKeyClusterAddr).String()
	fClusterProbeTimeout   = kingpin.Flag("agent-cluster-timeout", EnvKeyClusterProbeTimeout+" timeout interval for receiving agent member probe responses (only change this setting if you know what you're doing)").Envar(EnvKeyClusterProbeTimeout).Default(agent.DefaultClusterProbeTimeout).Duration()
	fClusterProbeInterval  = kingpin.Flag("agent-cluster-interval", EnvKeyClusterProbeInterval+" interval for repeating failed agent member probe (only change this setting if you know what you're doing)").Envar(EnvKeyClusterProbeInterval).Default(agent.DefaultClusterProbeInterval).Duration()
	fDataPath              = kingpin.Flag("data", EnvKeyDataPath+" path to the data folder").Envar(EnvKeyDataPath).Default(agent.DefaultDataPath).String()
	fSharedSecret          = kingpin.Flag("secret", EnvKeyAgentSecret+" shared secret used in the signature verification process").Envar(EnvKeyAgentSecret).String()
	fLogLevel              = kingpin.Flag("log-level", EnvKeyLogLevel+" defines the log output verbosity (default to INFO)").Envar(EnvKeyLogLevel).Default(agent.DefaultLogLevel).Enum("ERROR", "WARN", "INFO", "DEBUG")
	fHealthCheck           = kingpin.Flag("health-check", "run the agent in healthcheck mode and exit after running preflight checks").Envar(EnvKeyHealthCheck).Default("false").Bool()

	// Edge mode
	fEdgeMode              = kingpin.Flag("edge", EnvKeyEdge+" enable Edge mode. Disabled by default, set to 1 or true to enable it").Envar(EnvKeyEdge).Bool()
	fEdgeAsyncMode         = kingpin.Flag("edge-async", EnvKeyEdge+" enable Edge Async mode. Disabled by default, set to 1 or true to enable it").Envar(EnvKeyEdgeAsync).Bool()
	fEdgeKey               = kingpin.Flag("edge-key", EnvKeyEdgeKey+" specify an Edge key to use at startup").Envar(EnvKeyEdgeKey).String()
	fEdgeID                = kingpin.Flag("edge-id", EnvKeyEdgeID+" a unique identifier associated to this agent cluster").Envar(EnvKeyEdgeID).String()
	fEdgeServerAddr        = kingpin.Flag("edge-host", EnvKeyEdgeServerHost+" address on which the Edge UI will be exposed (default to 0.0.0.0)").Envar(EnvKeyEdgeServerHost).Default(agent.DefaultEdgeServerAddr).IP()
	fEdgeServerPort        = kingpin.Flag("edge-port", EnvKeyEdgeServerPort+" port on which the Edge UI will be exposed (default to 80)").Envar(EnvKeyEdgeServerPort).Default(agent.DefaultEdgeServerPort).Int()
	fEdgeInactivityTimeout = kingpin.Flag("edge-inactivity", EnvKeyEdgeInactivityTimeout+" timeout used by the agent to close the reverse tunnel after inactivity (default to 5m)").Envar(EnvKeyEdgeInactivityTimeout).Default(agent.DefaultEdgeSleepInterval).String()
	fEdgeInsecurePoll      = kingpin.Flag("edge-insecurepoll", EnvKeyEdgeInsecurePoll+" enable this option if you need the agent to poll a HTTPS Portainer instance with self-signed certificates. Disabled by default, set to 1 to enable it").Envar(EnvKeyEdgeInsecurePoll).Bool()
	fEdgeTunnel            = kingpin.Flag("edge-tunnel", EnvKeyEdgeTunnel+" disable this option if you wish to prevent the agent from opening tunnels over websockets").Envar(EnvKeyEdgeTunnel).Default("true").Bool()

	// mTLS edge agent certs
	fSSLCert           = kingpin.Flag("sslcert", "Path to the SSL certificate used to identify the agent to Portainer").Envar(EnvKeySSLCert).String()
	fSSLKey            = kingpin.Flag("sslkey", "Path to the SSL key used to identify the agent to Portainer").Envar(EnvKeySSLKey).String()
	fSSLCACert         = kingpin.Flag("sslcacert", "Path to the SSL CA certificate used to validate the Portainer server").Envar(EnvKeySSLCACert).String()
	fCertRetryInterval = kingpin.Flag("certificate-retry-interval", "Interval used to block initialization until the certificate is available").Envar(EnvKeyCertRetryInterval).Duration()
)

func (parser *EnvOptionParser) Options() (*agent.Options, error) {
	kingpin.Parse()

	return &agent.Options{
		AssetsPath:            *fAssetsPath,
		AgentServerAddr:       fAgentServerAddr.String(),
		AgentServerPort:       strconv.Itoa(*fAgentServerPort),
		AgentSecurityShutdown: *fAgentSecurityShutdown,
		ClusterAddress:        *fClusterAddress,
		ClusterProbeTimeout:   *fClusterProbeTimeout,
		ClusterProbeInterval:  *fClusterProbeInterval,
		DataPath:              *fDataPath,
		EdgeMode:              *fEdgeMode,
		EdgeAsyncMode:         *fEdgeAsyncMode,
		EdgeKey:               *fEdgeKey,
		EdgeID:                *fEdgeID,
		EdgeUIServerAddr:      fEdgeServerAddr.String(),
		EdgeUIServerPort:      strconv.Itoa(*fEdgeServerPort),
		EdgeInactivityTimeout: *fEdgeInactivityTimeout,
		EdgeInsecurePoll:      *fEdgeInsecurePoll,
		EdgeTunnel:            *fEdgeTunnel,
		HealthCheck:           *fHealthCheck,
		LogLevel:              *fLogLevel,
		SharedSecret:          *fSharedSecret,
		SSLCert:               *fSSLCert,
		SSLKey:                *fSSLKey,
		SSLCACert:             *fSSLCACert,
		CertRetryInterval:     *fCertRetryInterval,
	}, nil
}
