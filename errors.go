package agent

// Error represents an application error.
type Error string

// Error returns the error message.
func (e Error) Error() string { return string(e) }

// General errors.
const (
	ErrAgentNotFound        = Error("Unable to find the targeted agent")
	ErrManagerAgentNotFound = Error("Unable to find an agent on any manager node")
)
