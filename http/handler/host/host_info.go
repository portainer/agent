package host

import (
	"net/http"

	httperror "github.com/portainer/agent/http/error"
	"github.com/portainer/agent/http/response"
)

type hostInfo struct {
	PhysicalDeviceVendor string
	DeviceVersion        string
	DeviceSerialNumber   string
	InstalledPCIDevices  []string
	PhysicalDisk         string
}

func (handler *Handler) hostInfo(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	response.JSON(rw, hostInfo{
		PhysicalDeviceVendor: "hello",
	})
	return nil
}
