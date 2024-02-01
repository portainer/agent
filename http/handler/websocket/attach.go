package websocket

import (
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"

	"github.com/asaskevich/govalidator"
	"github.com/gorilla/websocket"
)

func (handler *Handler) websocketOperation(
	w http.ResponseWriter,
	r *http.Request,
	operation func(http.ResponseWriter, *http.Request) *httperror.HandlerError,
) *httperror.HandlerError {
	if handler.clusterService == nil {
		return operation(w, r)
	}

	agentTargetHeader := r.Header.Get(agent.HTTPTargetHeaderName)
	if agentTargetHeader == handler.runtimeConfiguration.NodeName {
		return operation(w, r)
	}

	targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
	if targetMember == nil {
		return httperror.InternalServerError("The agent was unable to contact any other agent", errors.New("Unable to find the targeted agent"))
	}

	proxy.WebsocketRequest(w, r, targetMember)
	return nil
}

func (handler *Handler) websocketAttach(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	return handler.websocketOperation(w, r, handler.handleAttachRequest)
}

func (handler *Handler) handleAttachRequest(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	attachID, err := request.RetrieveQueryParameter(r, "id", false)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: id", err)
	}
	if !govalidator.IsHexadecimal(attachID) {
		return httperror.BadRequest("Invalid query parameter: id (must be hexadecimal identifier)", err)
	}

	r.Header.Del("Origin")

	websocketConn, err := handler.connectionUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return httperror.InternalServerError("An error occurred during websocket attach operation: unable to upgrade connection", err)

	}
	defer websocketConn.Close()

	err = hijackAttachStartOperation(websocketConn, attachID)
	if err != nil {
		return httperror.InternalServerError("An error occurred during websocket attach operation", err)
	}

	return nil
}

func hijackAttachStartOperation(websocketConn *websocket.Conn, attachID string) error {
	dial, err := createDial()
	if err != nil {
		return err
	}

	// When we set up a TCP connection for hijack, there could be long periods
	// of inactivity (a long running command with no output) that in certain
	// network setups may cause ECONNTIMEOUT, leaving the client in an unknown
	// state. Setting TCP KeepAlive on the socket connection will prohibit
	// ECONNTIMEOUT unless the socket connection truly is broken
	if tcpConn, ok := dial.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	httpConn := httputil.NewClientConn(dial, nil)
	defer httpConn.Close()

	attachStartRequest, err := createAttachStartRequest(attachID)
	if err != nil {
		return err
	}

	return hijackRequest(websocketConn, httpConn, attachStartRequest)
}

func createAttachStartRequest(attachID string) (*http.Request, error) {
	r, err := http.NewRequest("POST", "/containers/"+attachID+"/attach?stdin=1&stdout=1&stderr=1&stream=1", nil)
	if err != nil {
		return nil, err
	}

	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "tcp")

	return r, nil
}
