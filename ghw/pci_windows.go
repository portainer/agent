// +build windows

package ghw

import (
	"errors"

	"github.com/portainer/agent"
)

// GetDiskInfo returns info about the host disks
func (service *SystemService) GetPciDevices() ([]agent.PciDevice, error) {
	return nil, errors.New("Platform not supported")
}
