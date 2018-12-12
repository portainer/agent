// +build windows

package ghw

import (
	"errors"

	"github.com/portainer/agent"
)

// GetDiskInfo returns info about the host disks
func (service *SystemService) GetDiskInfo() ([]agent.PhysicalDisk, error) {
	return nil, errors.New("Platform not supported")
}
