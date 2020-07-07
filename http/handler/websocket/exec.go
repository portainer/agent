package websocket

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/gorilla/websocket"
	"github.com/portainer/agent"
	"github.com/portainer/agent/http/proxy"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
)

func (handler *Handler) websocketExec(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	if handler.clusterService == nil {
		return handler.handleExecRequest(w, r)
	}

	agentTargetHeader := r.Header.Get(agent.HTTPTargetHeaderName)

	if agentTargetHeader == handler.runtimeConfiguration.NodeName {
		return handler.handleExecRequest(w, r)
	}

	targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
	if targetMember == nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "The agent was unable to contact any other agent", errors.New("Unable to find the targeted agent")}
	}

	proxy.WebsocketRequest(w, r, targetMember)
	return nil
}

func (handler *Handler) handleExecRequest(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	execID, err := request.RetrieveQueryParameter(r, "id", false)
	if execID == "" {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: id", err}
	}

	if !govalidator.IsHexadecimal(execID) {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: id (must be hexadecimal identifier)", err}
	}

	websocketConn, err := handler.connectionUpgrader.Upgrade(rw, r, nil)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "An error occured during websocket exec operation: unable to upgrade connection", err}
	}
	defer websocketConn.Close()

	err = hijackExecStartOperation(websocketConn, execID)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "An error occured during websocket exec hijack operation", err}
	}

	return nil
}

func hijackExecStartOperation(websocketConn *websocket.Conn, execID string) error {
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

	execStartRequest, err := createExecStartRequest(execID)
	if err != nil {
		return err
	}

	err = hijackRequest(websocketConn, httpConn, execStartRequest)
	if err != nil {
		return err
	}

	return nil
}

func createExecStartRequest(execID string) (*http.Request, error) {
	execStartOperationPayload := &execStartOperationPayload{
		Tty:    true,
		Detach: false,
	}

	encodedBody := bytes.NewBuffer(nil)
	err := json.NewEncoder(encodedBody).Encode(execStartOperationPayload)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("POST", "/exec/"+execID+"/start", encodedBody)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "tcp")

	return request, nil
}
