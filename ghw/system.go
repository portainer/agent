package ghw

// SystemService is used to get info about the host
type SystemService struct {
	hostRoot string
}

// NewSystemService returns a pointer to a new SystemService
func NewSystemService(hostRoot string) *SystemService {
	service := &SystemService{}
	service.hostRoot = hostRoot
	return service
}
