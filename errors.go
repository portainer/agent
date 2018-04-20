package agent

// Error represents an application error.
type Error string

// Error returns the error message.
func (e Error) Error() string { return string(e) }

// General errors.
const (
	ErrAgentNotFound        = Error("Unable to find the targeted agent")
	ErrManagerAgentNotFound = Error("Unable to find an agent on any manager node")
	ErrUnauthorized         = Error("Unauthorized")
)

// Agent setup errors.
const (
	ErrInvalidEnvPortFormat      = Error("Invalid port format in AGENT_PORT environment variable")
	ErrEnvClusterAddressRequired = Error("AGENT_CLUSTER_ADDR environment variable is required")
	ErrRetrievingAdvertiseAddr   = Error("Unable to retrieve the address on which the agent can advertise. Check your network settings")
)

// Agent runtime errors.
const (
	ErrPublicKeyUnavailable = Error("Portainer public key unavailable")
)
