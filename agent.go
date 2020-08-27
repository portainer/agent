package agent

type (
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

	// DockerEngineStatus represent the status of a Docker runtime (standalone or swarm)
	DockerEngineStatus int

	// DockerNodeRole represent the role of a Docker swarm node
	DockerNodeRole int

	// DockerRuntimeConfiguration represents the runtime configuration of an agent running on the Docker platform
	DockerRuntimeConfiguration struct {
		EngineStatus DockerEngineStatus
		Leader       bool
		NodeRole     DockerNodeRole
	}

	// EdgeStackConfig represent an Edge stack config
	EdgeStackConfig struct {
		Name        string
		FileContent string
		Prune       bool
	}

	// HostInfo is the representation of the collection of host information
	HostInfo struct {
		PCIDevices    []PciDevice
		PhysicalDisks []PhysicalDisk
	}

	// KubernetesRuntimeConfiguration represents the runtime configuration of an agent running on the Kubernetes platform
	KubernetesRuntimeConfiguration struct{}

	// AgentMetadata is the representation of the metadata object used to decorate
	// all the objects in the response of a Docker aggregated resource request.
	Metadata struct {
		Agent struct {
			NodeName string `json:"NodeName"`
		} `json:"Agent"`
	}

	// Options are the options used to start an agent.
	Options struct {
		AgentServerAddr       string
		AgentServerPort       string
		ClusterAddress        string
		HostManagementEnabled bool
		SharedSecret          string
		EdgeMode              bool
		EdgeKey               string
		EdgeID                string
		EdgeServerAddr        string
		EdgeServerPort        string
		EdgeInactivityTimeout string
		EdgeInsecurePoll      bool
		LogLevel              string
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

	// RuntimeConfiguration represent the configuration of an agent during runtime
	RuntimeConfiguration struct {
		AgentPort               string
		EdgeKeySet              bool
		NodeName                string
		DockerConfiguration     DockerRuntimeConfiguration
		KubernetesConfiguration KubernetesRuntimeConfiguration
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
		ServerAddr       string
		ServerFingerpint string
		RemotePort       string
		LocalAddr        string
		Credentials      string
	}

	// ClusterService is used to manage a cluster of agents.
	ClusterService interface {
		Create(advertiseAddr string, joinAddr []string) error
		Members() []ClusterMember
		Leave()
		GetMemberByRole(role DockerNodeRole) *ClusterMember
		GetMemberByNodeName(nodeName string) *ClusterMember
		GetMemberWithEdgeKeySet() *ClusterMember
		GetRuntimeConfiguration() *RuntimeConfiguration
		UpdateRuntimeConfiguration(runtimeConfiguration *RuntimeConfiguration) error
	}

	// DigitalSignatureService is used to validate digital signatures.
	DigitalSignatureService interface {
		VerifySignature(signature, key string) (bool, error)
	}

	// DockerInfoService is used to retrieve information from a Docker environment.
	DockerInfoService interface {
		GetRuntimeConfigurationFromDockerEngine() (*RuntimeConfiguration, error)
		GetContainerIpFromDockerEngine(containerName string, ignoreNonSwarmNetworks bool) (string, error)
		GetServiceNameFromDockerEngine(containerName string) (string, error)
	}

	// DockerStackService is a service used to deploy and remove Docker stacks
	DockerStackService interface {
		Login() error
		Logout() error
		Deploy(name, stackFileContent string, prune bool) error
		Remove(name string) error
	}

	// KubernetesInfoService is used to retrieve information from a Kubernetes environment.
	KubernetesInfoService interface {
		GetInformationFromKubernetesCluster() (*RuntimeConfiguration, error)
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
	}

	// SystemService is used to get info about the host
	SystemService interface {
		GetDiskInfo() ([]PhysicalDisk, error)
		GetPciDevices() ([]PciDevice, error)
	}

	// TLSService is used to create TLS certificates to use enable HTTPS.
	TLSService interface {
		GenerateCertsForHost(host string) error
	}
)

const (
	// Version represents the version of the agent.
	Version = "2.0.0"
	// APIVersion represents the version of the agent's API.
	APIVersion = "2"
	// DefaultAgentAddr is the default address used by the Agent API server.
	DefaultAgentAddr = "0.0.0.0"
	// DefaultAgentPort is the default port exposed by the Agent API server.
	DefaultAgentPort = "9001"
	// DefaultLogLevel is the default logging level.
	DefaultLogLevel = "INFO"
	// DefaultEdgeSecurityShutdown is the default time after which the Edge server will shutdown if no key is specified
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
	// SupportedDockerAPIVersion is the minimum Docker API version supported by the agent.
	SupportedDockerAPIVersion = "1.24"
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
	// HTTPResponseAgentHeaderName is the name of the header that is automatically added
	// to each agent response.
	HTTPResponseAgentHeaderName = "Portainer-Agent"
	// HTTPKubernetesSATokenHeaderName represent the name of the header containing a Kubernetes SA token
	HTTPKubernetesSATokenHeaderName = "X-PortainerAgent-SA-Token"
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
	// DataDirectory is the folder where the data associated to the agent is persisted.
	DataDirectory = "/data"
	// ScheduleScriptDirectory is the folder where schedules are saved on the host
	ScheduleScriptDirectory = "/opt/portainer/scripts"
	// EdgeKeyFile is the name of the file used to persist the Edge key associated to the agent.
	EdgeKeyFile = "agent_edge_key"
	// DockerBinaryPath is the path of the docker binary
	DockerBinaryPath = "/app"
	// EdgeStackFilesPath is the path where edge stack files are saved
	EdgeStackFilesPath = "/tmp/edge_stacks"
	// EdgeStackQueueSleepInterval is the interval used to check if there's an Edge stack to deploy
	EdgeStackQueueSleepInterval = "5s"
)

const (
	_ ContainerPlatform = iota
	// PlatformDocker represent the Docker platform (Standalone/Swarm)
	PlatformDocker
	// PlatformKubernetes represent the Kubernetes platform
	PlatformKubernetes
)

const (
	_ DockerEngineStatus = iota
	// EngineStatusStandalone represent a standalone Docker environment
	EngineStatusStandalone
	// EngineStatusSwarm represent a Docker swarm environment
	EngineStatusSwarm
)

const (
	_ DockerNodeRole = iota
	// NodeRoleManager represent a Docker swarm manager node role
	NodeRoleManager
	// NodeRoleWorker represent a Docker swarm worker node role
	NodeRoleWorker
)
