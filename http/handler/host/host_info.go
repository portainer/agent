package host

import (
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

func (handler *Handler) hostInfo(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var hostInfo agent.HostInfo

	if err := handler.fillHostInfo(&hostInfo); err != nil {
		return httperror.InternalServerError("Unable to retrieve host information", err)
	}

	return response.JSON(rw, hostInfo)
}

func (handler *Handler) fillHostInfo(hi *agent.HostInfo) error {
	devices, devicesError := handler.systemService.GetPciDevices()
	if devicesError != nil {
		return devicesError
	}

	hi.PCIDevices = devices

	disks, disksError := handler.systemService.GetDiskInfo()
	if disksError != nil {
		return disksError
	}

	hi.PhysicalDisks = disks

	return nil
}
