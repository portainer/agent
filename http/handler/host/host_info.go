package host

import (
	"net/http"

	"github.com/jaypipes/ghw"
	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

type pciDevice struct {
	Vendor string
	Name   string
}

type physicalDisk struct {
	Vendor string
	Size   uint64
}

type hostInfo struct {
	PCIDevices    []pciDevice
	PhysicalDisks []physicalDisk
}

func (handler *Handler) hostInfo(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var hi hostInfo

	err := fillHostInfo(&hi)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Can't get host info", err}
	}
	response.JSON(rw, hi)
	return nil
}

func fillHostInfo(hi *hostInfo) error {
	devices, err := fillPciDevices()
	if err != nil {
		return err
	}
	hi.PCIDevices = devices

	disks, err := fillPhysicalDisk()
	if err != nil {
		return err
	}
	hi.PhysicalDisks = disks
	return nil

}

func fillPciDevices() ([]pciDevice, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, err
	}

	devicesRaw := pci.ListDevices()

	if len(devicesRaw) == 0 {
		return nil, agent.Error("Could not retrieve PCI devices")
	}

	var devices []pciDevice
	for _, device := range devicesRaw {
		devices = append(devices, pciDevice{
			Vendor: device.Vendor.Name,
			Name:   device.Product.Name,
		})

	}

	return devices, nil
}

func fillPhysicalDisk() ([]physicalDisk, error) {
	block, err := ghw.Block()
	if err != nil {
		return nil, err
	}

	var disks []physicalDisk

	for _, disk := range block.Disks {
		disks = append(disks, physicalDisk{
			Vendor: disk.Vendor,
			Size:   disk.SizeBytes,
		})
	}

	return disks, nil
}
