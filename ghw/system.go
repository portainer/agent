package ghw

import (
	"github.com/jaypipes/ghw"

	"github.com/portainer/agent"
)

// SystemService is used to get info about the host
type SystemService struct {
}

// NewSystemService returns a pointer to a new SystemService
func NewSystemService() *SystemService {
	return &SystemService{}
}

// GetDiskInfo returns info about the host disks
func (service *SystemService) GetDiskInfo() ([]agent.PhysicalDisk, error) {
	block, err := ghw.Block()
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

// GetPciDevices returns a list of pci devices
func (service *SystemService) GetPciDevices() ([]agent.PciDevice, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, err
	}

	devicesRaw := pci.ListDevices()

	if len(devicesRaw) == 0 {
		return nil, agent.Error("Could not retrieve PCI devices")
	}

	var devices []agent.PciDevice
	for _, device := range devicesRaw {
		devices = append(devices, agent.PciDevice{
			Vendor: device.Vendor.Name,
			Name:   device.Product.Name,
		})

	}

	return devices, nil
}
