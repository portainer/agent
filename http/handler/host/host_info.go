package host

import (
	"net/http"

	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
)

func (handler *Handler) hostInfo(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var hi agent.HostInfo

	err := handler.fillHostInfo(&hi)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to retrieve host information", err}
	}
	response.JSON(rw, hi)
	return nil
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
