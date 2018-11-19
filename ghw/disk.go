// +build !windows

package ghw

import (
	"github.com/jaypipes/ghw"
	"github.com/portainer/agent"
)

// GetDiskInfo returns info about the host disks
func (service *SystemService) GetDiskInfo() ([]agent.PhysicalDisk, error) {
	block, err := ghw.Block(ghw.WithChroot(service.hostRoot))
	if err != nil {
		return nil, err
	}

	var disks []agent.PhysicalDisk

	for _, disk := range block.Disks {
		disks = append(disks, agent.PhysicalDisk{
			Vendor: disk.Vendor,
			Size:   disk.SizeBytes,
		})
	}

	return disks, nil
}
