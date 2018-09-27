// +build !windows

package ghw

import (
	"demo-ghw/ghw"

	"github.com/portainer/agent"
)

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
