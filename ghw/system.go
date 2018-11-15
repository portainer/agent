package ghw

import "os"

// SystemService is used to get info about the host
type SystemService struct {
	hostRoot string
}

// NewSystemService returns a pointer to a new SystemService
func NewSystemService(hostRoot string) *SystemService {
	// TODO should be passed to `PCI()` instead of setting the envvar
	// TODO should we clean this var?
	os.Setenv("PCIDB_CHROOT", hostRoot)
	service := &SystemService{}
	service.hostRoot = hostRoot
	return service
}
