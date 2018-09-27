package host

import (
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) hostInfo(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var hostInfo agent.HostInfo

	err := handler.fillHostInfo(&hostInfo)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to retrieve host information", err}
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
