package agent

import (
	"context"
	"time"

	"github.com/portainer/portainer/pkg/libstack"
)

type (
	// AWSConfig is a configuration used to authenticate against AWS IAM Roles Anywhere
	AWSConfig struct {
		ClientCertPath   string
		ClientKeyPath    string
		ClientBundlePath string
		RoleARN          string
		TrustAnchorARN   string
		ProfileARN       string
		Region           string
	}

	// ClusterMember is the representation of an agent inside a cluster.
	ClusterMember struct {
		IPAddress  string
		Port       string
		NodeName   string
		NodeRole   string
		EdgeKeySet bool
	}

	// ContainerPlatform represent the platform on which the agent is running (Docker, Kubernetes)
	ContainerPlatform int

	// DockerEngineType represent the type of a Docker runtime (standalone or swarm)
	DockerEngineType int

	// DockerNodeRole represent the role of a Docker swarm node
	DockerNodeRole int

	// DockerRuntimeConfig represents the runtime configuration of an agent running on the Docker platform
	DockerRuntimeConfig struct {
		EngineType DockerEngineType
		Leader     bool
		NodeRole   DockerNodeRole
	}

	// EdgeJobStatus represents an Edge job status
	EdgeJobStatus struct {
		JobID          int    `json:"JobID"`
		LogFileContent string `json:"LogFileContent"`
	}

	// HostInfo is the representation of the collection of host information
	HostInfo struct {
		PCIDevices    []PciDevice
		PhysicalDisks []PhysicalDisk
	}

	// KubernetesRuntimeConfig represents the runtime configuration of an agent running on the Kubernetes platform
	KubernetesRuntimeConfig struct{}

	// AgentMetadata is the representation of the metadata object used to decorate
	// all the objects in the response of a Docker aggregated resource request.
	Metadata struct {
		Agent struct {
			NodeName string `json:"NodeName"`
		} `json:"Agent"`
	}

	EdgeMetaFields struct {
		// EdgeGroupsIDs - Used for AEEC, the created environment will be added to these edge groups
		EdgeGroupsIDs []int
		// EnvironmentGroupID - Used for AEEC, the created environment will be added to this edge group
		EnvironmentGroupID int
		// TagsIDs - Used for AEEC, the created environment will be added to these edge tags
		TagsIDs  []int
		UpdateID int
	}

	// Options are the options used to start an agent.
	Options struct {
		AssetsPath            string
		AgentServerAddr       string
		AgentServerPort       string
		AgentSecurityShutdown time.Duration
		ClusterAddress        string
		ClusterProbeTimeout   time.Duration
		ClusterProbeInterval  time.Duration
		DataPath              string
		SharedSecret          string
		EdgeMode              bool
		EdgeAsyncMode         bool
		EdgeKey               string
		EdgeID                string
		EdgeUIServerAddr      string
		EdgeUIServerPort      string
		EdgeInactivityTimeout string
		EdgeInsecurePoll      bool
		EdgeTunnel            bool
		EdgeTunnelProxy       string
		EdgeMetaFields        EdgeMetaFields
		LogLevel              string
		LogMode               string
		SSLCert               string
		SSLKey                string
		SSLCACert             string
		CertRetryInterval     time.Duration
		AWSClientCert         string
		AWSClientKey          string
		AWSClientBundle       string
		AWSRoleARN            string
		AWSTrustAnchorARN     string
		AWSProfileARN         string
		AWSRegion             string
	}

	// PciDevice is the representation of a physical pci device on a host
	PciDevice struct {
		Vendor string
		Name   string
	}

	// PhysicalDisk is the representation of a physical disk on a host
	PhysicalDisk struct {
		Vendor string
		Size   uint64
	}

	// RuntimeConfig represent the configuration of an agent during runtime
	RuntimeConfig struct {
		AgentPort        string
		EdgeKeySet       bool
		NodeName         string
		DockerConfig     DockerRuntimeConfig
		KubernetesConfig KubernetesRuntimeConfig
	}

	// Schedule represents a script that can be scheduled on the underlying host
	Schedule struct {
		ID             int
		CronExpression string
		Script         string
		Version        int
		CollectLogs    bool
	}

	// TunnelConfig contains all the required information for the agent to establish
	// a reverse tunnel to a Portainer instance
	TunnelConfig struct {
		ServerAddr        string
		ServerFingerprint string
		RemotePort        string
		LocalAddr         string
		Credentials       string
		// Proxy is the proxy URL to use for the tunnel connection
		Proxy string
	}

	// ClusterService is used to manage a cluster of agents.
	ClusterService interface {
		Create(advertiseAddr string, joinAddr []string, probeTimeout, probeInterval time.Duration) error
		Members() []ClusterMember
		Leave()
		GetMemberByRole(role DockerNodeRole) *ClusterMember
		GetMemberByNodeName(nodeName string) *ClusterMember
		GetMemberWithEdgeKeySet() *ClusterMember
		GetRuntimeConfiguration() *RuntimeConfig
		UpdateRuntimeConfiguration(runtimeConfiguration *RuntimeConfig) error
	}

	// DigitalSignatureService is used to validate digital signatures.
	DigitalSignatureService interface {
		IsAssociated() bool
		VerifySignature(signature, key string) (bool, error)
	}

	// DockerInfoService is used to retrieve information from a Docker environment.
	DockerInfoService interface {
		GetRuntimeConfigurationFromDockerEngine() (*RuntimeConfig, error)
		GetContainerIpFromDockerEngine(containerName string, ignoreNonSwarmNetworks bool) (string, error)
		GetServiceNameFromDockerEngine(containerName string) (string, error)
	}

	Deployer interface {
		Deploy(ctx context.Context, name string, filePaths []string, options DeployOptions) error
		Remove(ctx context.Context, name string, filePaths []string, options RemoveOptions) error
		Pull(ctx context.Context, name string, filePaths []string, options PullOptions) error
		Validate(ctx context.Context, name string, filePaths []string, options ValidateOptions) error
		// WaitForStatus waits until status is reached or an error occurred
		// if the received value is an empty string it means the status was
		WaitForStatus(ctx context.Context, name string, status libstack.Status) <-chan libstack.WaitResult
	}

	DeployerBaseOptions struct {
		// Namespace to use for kubernetes stack. Keep empty to use the manifest namespace.
		Namespace  string
		WorkingDir string
		Env        []string
	}

	DeployOptions struct {
		DeployerBaseOptions
		Prune         bool
		ForceRecreate bool
	}

	RemoveOptions struct {
		DeployerBaseOptions
	}

	ValidateOptions struct {
		DeployerBaseOptions
	}

	PullOptions struct {
		DeployerBaseOptions
	}

	// KubernetesInfoService is used to retrieve information from a Kubernetes environment.
	KubernetesInfoService interface {
		GetInformationFromKubernetesCluster() (*RuntimeConfig, error)
	}

	// OptionParser is used to parse options.
	OptionParser interface {
		Options() (*Options, error)
	}

	// ReverseTunnelClient is used to create a reverse proxy tunnel when
	// the agent is started in Edge mode.
	ReverseTunnelClient interface {
		CreateTunnel(config TunnelConfig) error
		CloseTunnel() error
		IsTunnelOpen() bool
	}

	// Scheduler is used to manage schedules
	Scheduler interface {
		Schedule(schedules []Schedule) error
		AddSchedule(schedule Schedule) error
		RemoveSchedule(schedule Schedule) error
		ProcessScheduleLogsCollection()
	}

	// SystemService is used to get info about the host
	SystemService interface {
		GetDiskInfo() ([]PhysicalDisk, error)
		GetPciDevices() ([]PciDevice, error)
	}
)

const (
	// Version represents the version of the agent.
	Version = "2.22.0"
)

const (
	// APIVersion represents the version of the agent's API.
	APIVersion = "2"
	// DefaultAgentAddr is the default address used by the Agent API server.
	DefaultAgentAddr = "0.0.0.0"
	// DefaultAgentPort is the default port exposed by the Agent API server.
	DefaultAgentPort = "9001"
	// DefaultLogLevel is the default logging level.
	DefaultLogLevel = "INFO"
	// DefaultAgentSecurityShutdown is the default time after which the API server will shut down if not associated with a Portainer instance
	DefaultAgentSecurityShutdown = "72h"
	// DefaultEdgeSecurityShutdown is the default time after which the Edge server will shut down if no key is specified
	DefaultEdgeSecurityShutdown = 15
	// DefaultEdgeServerAddr is the default address used by the Edge server.
	DefaultEdgeServerAddr = "0.0.0.0"
	// DefaultEdgeServerPort is the default port exposed by the Edge server.
	DefaultEdgeServerPort = "80"
	// DefaultEdgePollInterval is the default interval used to poll Edge information from a Portainer instance.
	DefaultEdgePollInterval = "5s"
	// DefaultEdgeSleepInterval is the default interval after which the agent will close the tunnel if no activity.
	DefaultEdgeSleepInterval = "5m"
	// DefaultConfigCheckInterval is the default interval used to check if node config changed
	DefaultConfigCheckInterval = "5s"
	// DefaultClusterProbeTimeout is the default member list ping probe timeout.
	DefaultClusterProbeTimeout = "500ms"
	// DefaultClusterProbeInterval is the interval for repeating failed node checks.
	DefaultClusterProbeInterval = "1s"
	// HTTPTargetHeaderName is the name of the header used to specify a target node.
	HTTPTargetHeaderName = "X-PortainerAgent-Target"
	// HTTPEdgeIdentifierHeaderName is the name of the header used to specify the Docker identifier associated to
	// an Edge agent.
	HTTPEdgeIdentifierHeaderName = "X-PortainerAgent-EdgeID"
	// HTTPManagerOperationHeaderName is the name of the header used to specify that
	// a request must target a manager node.
	HTTPManagerOperationHeaderName = "X-PortainerAgent-ManagerOperation"
	// HTTPSignatureHeaderName is the name of the header containing the digital signature
	// of a Portainer instance.
	HTTPSignatureHeaderName = "X-PortainerAgent-Signature"
	// HTTPPublicKeyHeaderName is the name of the header containing the public key
	// of a Portainer instance.
	HTTPPublicKeyHeaderName = "X-PortainerAgent-PublicKey"
	// HTTPResponseAgentTimeZone is the name of the header containing the timezone
	HTTPResponseAgentTimeZone = "X-PortainerAgent-TimeZone"
	// HTTPResponseUpdateIDHeaderName is the name of the header that will have the update ID that started this container
	HTTPResponseUpdateIDHeaderName = "X-PortainerAgent-Update-ID"
	// HTTPResponseAgentHeaderName is the name of the header that is automatically added
	// to each agent response.
	HTTPResponseAgentHeaderName = "Portainer-Agent"
	// HTTPKubernetesSATokenHeaderName represent the name of the header containing a Kubernetes SA token
	HTTPKubernetesSATokenHeaderName = "X-PortainerAgent-SA-Token"
	// PortainerUpdaterEnv is custom environment variable used to identify if a task runs portainer-updater
	PortainerUpdaterEnv = "PORTAINER_UPDATER"
	// HTTPResponseAgentApiVersion is the name of the header that will have the
	// Portainer Agent API Version.
	HTTPResponseAgentApiVersion = "Portainer-Agent-API-Version"
	// HTTPResponseAgentPlatform is the name of the header that will have the Portainer agent platform
	HTTPResponseAgentPlatform = "Portainer-Agent-Platform"
	// PortainerAgentSignatureMessage is the unhashed content that is signed by the Portainer instance.
	// It is used by the agent during the signature verification process.
	PortainerAgentSignatureMessage = "Portainer-App"
	// ResponseMetadataKey is the JSON field used to store any Portainer related information in
	// response objects.
	ResponseMetadataKey = "Portainer"
	// TLSCertPath is the default path to the TLS certificate file.
	TLSCertPath = "cert.pem"
	// TLSKeyPath is the default path to the TLS key file.
	TLSKeyPath = "key.pem"
	// HostRoot is the folder mapping to the underlying host filesystem that is mounted inside the container.
	HostRoot = "/host"
	// DefaultDataPath is the default folder where the data associated to the agent is persisted.
	DefaultDataPath = "/data"
	// ScheduleScriptDirectory is the folder where schedules are saved on the host
	ScheduleScriptDirectory = "/opt/portainer/scripts"
	// EdgeKeyFile is the name of the file used to persist the Edge key associated to the agent.
	EdgeKeyFile = "agent_edge_key"
	// DefaultAssetsPath is the default path of the binaries
	DefaultAssetsPath = "/app"
	// EdgeStackFilesPath is the path where edge stack files are saved
	EdgeStackFilesPath = "/tmp/edge_stacks"
	// EdgeStackQueueSleepIntervalSeconds is the interval in seconds used to check if there's an Edge stack to deploy
	EdgeStackQueueSleepIntervalSeconds = 5
	// KubernetesServiceHost is the environment variable name of the kubernetes API server host
	KubernetesServiceHost = "KUBERNETES_SERVICE_HOST"
	// KubernetesServicePortHttps is the environment variable of the kubernetes API server https port
	KubernetesServicePortHttps = "KUBERNETES_SERVICE_PORT_HTTPS"
	// DefaultAWSClientCertPath is the default path to the AWS client certificate file
	DefaultAWSClientCertPath = "/certs/aws-client.crt"
	// DefaultAWSClientKeyPath is the default path to the AWS client key file
	DefaultAWSClientKeyPath = "/certs/aws-client.key"
	// DefaultUnpackerImage is the default name of unpacker image
	DefaultUnpackerImage = "portainer/compose-unpacker:" + Version
	// ComposeUnpackerImageEnvVar is the default environment variable name of the unpacker image
	ComposeUnpackerImageEnvVar = "COMPOSE_UNPACKER_IMAGE"
	// ComposePathPrefix is the folder name of compose path in unpacker
	ComposePathPrefix = "portainer-compose-unpacker"
	// EdgeIdEnvVarName is the environment variable name of the edge ID for per device edge stack configurations
	EdgeIdEnvVarName = "PORTAINER_EDGE_ID"
)

const (
	_ ContainerPlatform = iota
	// PlatformDocker represent the Docker platform (Standalone/Swarm)
	PlatformDocker
	// PlatformKubernetes represent the Kubernetes platform
	PlatformKubernetes
	// PlatformPodman represent the Podman platform (Standalone)
	PlatformPodman
	// Deprecated: PlatformNomad represent the Nomad platform (Standalone)
	PlatformNomad
)

const (
	_ DockerEngineType = iota
	// EngineTypeStandalone represent a standalone Docker environment
	EngineTypeStandalone
	// EngineTypeSwarm represent a Docker swarm environment
	EngineTypeSwarm
)

const (
	_ DockerNodeRole = iota
	// NodeRoleManager represent a Docker swarm manager node role
	NodeRoleManager
	// NodeRoleWorker represent a Docker swarm worker node role
	NodeRoleWorker
)

const (
	// TunnelStatusIdle represents an idle state for a tunnel connected to an Edge environment(endpoint).
	TunnelStatusIdle string = "IDLE"
	// TunnelStatusRequired represents a required state for a tunnel connected to an Edge environment(endpoint)
	TunnelStatusRequired string = "REQUIRED"
	// TunnelStatusActive represents an active state for a tunnel connected to an Edge environment(endpoint)
	TunnelStatusActive string = "ACTIVE"
)
