package websocket

import (
	"bytes"
	"encoding/json"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"

	"github.com/asaskevich/govalidator"
	"github.com/gorilla/websocket"
)

func (handler *Handler) websocketExec(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	return handler.websocketOperation(w, r, handler.handleExecRequest)
}

func (handler *Handler) handleExecRequest(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	execID, err := request.RetrieveQueryParameter(r, "id", false)
	if execID == "" {
		return httperror.BadRequest("Invalid query parameter: id", err)
	}

	if !govalidator.IsHexadecimal(execID) {
		return httperror.BadRequest("Invalid query parameter: id (must be hexadecimal identifier)", err)
	}

	websocketConn, err := handler.connectionUpgrader.Upgrade(rw, r, nil)
	if err != nil {
		return httperror.InternalServerError("An error occurred during websocket exec operation: unable to upgrade connection", err)
	}
	defer websocketConn.Close()

	err = hijackExecStartOperation(websocketConn, execID)
	if err != nil {
		return httperror.InternalServerError("An error occurred during websocket exec hijack operation", err)
	}

	return nil
}

func hijackExecStartOperation(websocketConn *websocket.Conn, execID string) error {
	return hijackStartOperation(websocketConn, execID, createExecStartRequest)
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
