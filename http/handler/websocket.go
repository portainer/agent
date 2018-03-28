package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"bitbucket.org/portainer/agent"
	httperror "bitbucket.org/portainer/agent/http/error"
	"bitbucket.org/portainer/agent/http/operations"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type (
	// WebSocketHandler represents an HTTP API handler for proxying requests to a web socket.
	WebSocketHandler struct {
		*mux.Router
		logger             *log.Logger
		clusterService     agent.ClusterService
		connectionUpgrader websocket.Upgrader
	}

	execStartOperationPayload struct {
		Tty    bool
		Detach bool
	}
)

// NewWebSocketHandler returns a new instance of WebSocketHandler.
func NewWebSocketHandler(clusterService agent.ClusterService) *WebSocketHandler {
	h := &WebSocketHandler{
		Router:             mux.NewRouter(),
		logger:             log.New(os.Stderr, "", log.LstdFlags),
		connectionUpgrader: websocket.Upgrader{},
		clusterService:     clusterService,
	}
	h.HandleFunc("/websocket/exec", h.handleWebsocketExec)
	return h
}

func (handler *WebSocketHandler) handleWebsocketExec(w http.ResponseWriter, r *http.Request) {

	agentOperationHeader := r.Header.Get(agent.HTTPOperationHeaderName)
	agentTargetHeader := r.Header.Get(agent.HTTPTargetHeaderName)

	if agentOperationHeader == agent.HTTPOperationHeaderValue || agentTargetHeader == "" {
		execID := r.FormValue("id")
		if execID == "" {
			httperror.WriteErrorResponse(w, errInvalidQueryParameters, http.StatusBadRequest, handler.logger)
			return
		}

		err := handler.handleRequest(w, r, execID)
		if err != nil {
			httperror.WriteErrorResponse(w, err, http.StatusInternalServerError, handler.logger)
			return
		}
	} else {
		targetMember := handler.clusterService.GetMemberByNodeName(agentTargetHeader)
		// TODO: find something to do when the targeted member is not found inside the cluster
		if targetMember == nil {
			httperror.WriteErrorResponse(w, agent.Error("Unable to find the agent."),
				http.StatusInternalServerError, handler.logger)
		}

		operations.NodeWSOperation(w, r, targetMember)
	}
}

func (handler *WebSocketHandler) handleRequest(w http.ResponseWriter, r *http.Request, execID string) error {
	websocketConn, err := handler.connectionUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer websocketConn.Close()

	return hijackExecStartOperation(websocketConn, execID)
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

func createDial() (net.Conn, error) {
	return net.Dial("unix", "/var/run/docker.sock")
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

func hijackRequest(websocketConn *websocket.Conn, httpConn *httputil.ClientConn, request *http.Request) error {
	// Server hijacks the connection, error 'connection closed' expected
	resp, err := httpConn.Do(request)
	if err != httputil.ErrPersistEOF {
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			resp.Body.Close()
			return fmt.Errorf("unable to upgrade to tcp, received %d", resp.StatusCode)
		}
	}

	tcpConn, brw := httpConn.Hijack()
	defer tcpConn.Close()

	errorChan := make(chan error, 1)
	go streamFromTCPConnToWebsocketConn(websocketConn, brw, errorChan)
	go streamFromWebsocketConnToTCPConn(websocketConn, tcpConn, errorChan)

	err = <-errorChan
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return err
	}

	return nil
}

func streamFromWebsocketConnToTCPConn(websocketConn *websocket.Conn, tcpConn net.Conn, errorChan chan error) {
	for {
		_, in, err := websocketConn.ReadMessage()
		if err != nil {
			errorChan <- err
			break
		}

		_, err = tcpConn.Write(in)
		if err != nil {
			errorChan <- err
			break
		}
	}
}

func streamFromTCPConnToWebsocketConn(websocketConn *websocket.Conn, br *bufio.Reader, errorChan chan error) {
	for {
		out := make([]byte, 1024)
		_, err := br.Read(out)
		if err != nil {
			errorChan <- err
			break
		}

		err = websocketConn.WriteMessage(websocket.TextMessage, out)
		if err != nil {
			errorChan <- err
			break
		}
	}
}
