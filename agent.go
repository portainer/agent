package agent

type (
	AgentOptions struct {
		Port               string
		ClusterAddress     string
		LogLevel           string
		PortainerPublicKey string
	}

	ClusterMember struct {
		IPAddress string
		Port      string
		NodeName  string
		NodeRole  string
	}

	AgentMetadata struct {
		Agent struct {
			NodeName string `json:"NodeName"`
		} `json:"Agent"`
	}

	ClusterService interface {
		Create(advertiseAddr, joinAddr string, tags map[string]string) error
		Members() []ClusterMember
		Leave()
		GetMemberByRole(role string) *ClusterMember
		GetMemberByNodeName(nodeName string) *ClusterMember
	}

	DigitalSignatureService interface {
		ParsePublicKey(key string) error
		ValidSignature(signature string) bool
	}

	InfoService interface {
		GetInformationFromDockerEngine() (map[string]string, error)
	}

	TLSService interface {
		GenerateCertsForHost(host string) error
	}
)

const (
	AgentVersion                   = "0.1.0"
	DefaultListenAddr              = "0.0.0.0"
	DefaultAgentPort               = "9001"
	DefaultLogLevel                = "INFO"
	SupportedDockerAPIVersion      = "1.24"
	HTTPTargetHeaderName           = "X-PortainerAgent-Target"
	HTTPManagerOperationHeaderName = "X-PortainerAgent-ManagerOperation"
	HTTPSignatureHeaderName        = "X-PortainerAgent-Signature"
	HTTPResponseAgentHeaderName    = "Portainer-Agent"
	PortainerAgentSignatureMessage = "Portainer-App"
	ResponseMetadataKey            = "Portainer"
	MemberTagKeyAgentPort          = "AgentPort"
	MemberTagKeyNodeName           = "NodeName"
	MemberTagKeyNodeRole           = "NodeRole"
	NodeRoleManager                = "manager"
	NodeRoleWorker                 = "worker"
	TLSCertPath                    = "cert.pem"
	TLSKeyPath                     = "key.pem"
)
