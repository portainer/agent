// +build !windows

package ghw

import (
	"github.com/jaypipes/ghw"
	"github.com/portainer/agent"
)

// GetPciDevices returns a list of pci devices
func (service *SystemService) GetPciDevices() ([]agent.PciDevice, error) {
	pci, err := ghw.PCI(ghw.WithChroot(service.hostRoot))
	if err != nil {
		return nil, err
	}

	devicesRaw := pci.ListDevices()

	devices := []agent.PciDevice{}
	for _, device := range devicesRaw {
		devices = append(devices, agent.PciDevice{
			Vendor: device.Vendor.Name,
			Name:   device.Product.Name,
		})

	}

	return devices, nil
}
