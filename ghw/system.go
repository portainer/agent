package ghw

// SystemService is used to get info about the host
type SystemService struct {
}

// NewSystemService returns a pointer to a new SystemService
func NewSystemService() *SystemService {
	return &SystemService{}
}
