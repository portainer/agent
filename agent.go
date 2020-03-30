package agent

type (
	// AgentOptions are the options used to start an agent.
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

	// ClusterMember is the representation of an agent inside a cluster.
	ClusterMember struct {
		IPAddress  string
		Port       string
		NodeName   string
		NodeRole   string
		EdgeKeySet bool
	}

	// AgentMetadata is the representation of the metadata object used to decorate
	// all the objects in the response of a Docker aggregated resource request.
	Metadata struct {
		Agent struct {
			NodeName string `json:"NodeName"`
		} `json:"Agent"`
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

	// HostInfo is the representation of the collection of host information
	HostInfo struct {
		PCIDevices    []PciDevice
		PhysicalDisks []PhysicalDisk
	}

	// Schedule represents a script that can be scheduled on the underlying host
	Schedule struct {
		ID             int
		CronExpression string
		Script         string
		Version        int
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

	// ContainerPlatform represent the platform on which the agent is running (Docker, Kubernetes)
	ContainerPlatform int

	// OptionParser is used to parse options.
	OptionParser interface {
		Options() (*Options, error)
	}

	// ClusterService is used to manage a cluster of agents.
	ClusterService interface {
		Create(advertiseAddr string, joinAddr []string) error
		Members() []ClusterMember
		Leave()
		GetMemberByRole(role string) *ClusterMember
		GetMemberByNodeName(nodeName string) *ClusterMember
		GetMemberWithEdgeKeySet() *ClusterMember
		GetTags() map[string]string
		UpdateTags(tags map[string]string) error
	}

	// DigitalSignatureService is used to validate digital signatures.
	DigitalSignatureService interface {
		VerifySignature(signature, key string) (bool, error)
	}

	// InfoService is used to retrieve information from a Docker environment.
	InfoService interface {
		GetInformationFromDockerEngine() (map[string]string, error)
		GetContainerIpFromDockerEngine(containerName string, ignoreNonSwarmNetworks bool) (string, error)
		GetServiceNameFromDockerEngine(containerName string) (string, error)
	}

	// TLSService is used to create TLS certificates to use enable HTTPS.
	TLSService interface {
		GenerateCertsForHost(host string) error
	}

	// SystemService is used to get info about the host
	SystemService interface {
		GetDiskInfo() ([]PhysicalDisk, error)
		GetPciDevices() ([]PciDevice, error)
	}

	// ReverseTunnelClient is used to create a reverse proxy tunnel when
	// the agent is started in Edge mode.
	ReverseTunnelClient interface {
		CreateTunnel(config TunnelConfig) error
		CloseTunnel() error
		IsTunnelOpen() bool
	}

	// TunnelOperator is a service that is used to communicate with a Portainer instance and to manage
	// the reverse tunnel.
	TunnelOperator interface {
		Start() error
		IsKeySet() bool
		SetKey(key string) error
		GetKey() string
		CloseTunnel() error
		ResetActivityTimer()
	}

	// Scheduler is used to manage schedules
	Scheduler interface {
		Schedule(schedules []Schedule) error
	}
)

const (
	// Version represents the version of the agent.
	Version = "1.5.1-k8s-beta"
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
	// PortainerAgentSignatureMessage is the unhashed content that is signed by the Portainer instance.
	// It is used by the agent during the signature verification process.
	PortainerAgentSignatureMessage = "Portainer-App"
	// ResponseMetadataKey is the JSON field used to store any Portainer related information in
	// response objects.
	ResponseMetadataKey = "Portainer"
	// MemberTagKeyAgentPort is the name of the label storing information about the port exposed
	// by the agent.
	MemberTagKeyAgentPort = "AgentPort"
	// MemberTagKeyNodeName is the name of the label storing information about the name of the
	// node where the agent is running.
	MemberTagKeyNodeName = "NodeName"
	// MemberTagKeyNodeRole is the name of the label storing information about the role of the
	// node where the agent is running.
	MemberTagKeyNodeRole = "NodeRole"
	// MemberTagEngineStatus is the name of the label storing information about the status of the Docker engine where
	// the agent is running. Possible values are "standalone" or "swarm".
	MemberTagEngineStatus = "EngineStatus"
	// MemberTagEdgeKeySet is the name of the label storing information regarding the association of an Edge key.
	MemberTagEdgeKeySet = "EdgeKeySet"
	// NodeRoleManager represents a manager node.
	NodeRoleManager = "manager"
	// NodeRoleWorker represents a worker node.
	NodeRoleWorker = "worker"
	// TLSCertPath is the default path to the TLS certificate file.
	TLSCertPath = "cert.pem"
	// TLSKeyPath is the default path to the TLS key file.
	TLSKeyPath = "key.pem"
	// HostRoot is the folder mapping to the underlying host filesystem that is mounted inside the container.
	HostRoot = "/host"
	// DataDirectory is the folder where the data associated to the agent is persisted.
	DataDirectory = "/data"
	// EdgeKeyFile is the name of the file used to persist the Edge key associated to the agent.
	EdgeKeyFile = "agent_edge_key"
)

const (
	_ ContainerPlatform = iota
	// PlatformDocker represent the Docker platform (Standalone/Swarm)
	PlatformDocker
	// PlatformKubernetes represent the Kubernetes platform
	PlatformKubernetes
)
