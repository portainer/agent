package websocket

import (
	"errors"
	"io"
	"net/http"

	"github.com/portainer/agent"
	"github.com/portainer/agent/kubernetes"
	kubecli "github.com/portainer/portainer/api/kubernetes/cli"
	"github.com/portainer/portainer/api/logs"
	"github.com/portainer/portainer/api/ws"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

func (handler *Handler) websocketPodExec(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	namespace, err := request.RetrieveQueryParameter(r, "namespace", false)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: namespace", err)
	}

	podName, err := request.RetrieveQueryParameter(r, "podName", false)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: podName", err)
	}

	containerName, err := request.RetrieveQueryParameter(r, "containerName", false)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: containerName", err)
	}

	command, err := request.RetrieveQueryParameter(r, "command", false)
	if err != nil {
		return httperror.BadRequest("Invalid query parameter: command", err)
	}

	token := r.Header.Get(agent.HTTPKubernetesSATokenHeaderName)

	commandArray := ws.SplitExecCommand(command)

	websocketConn, err := handler.connectionUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return httperror.InternalServerError("Unable to upgrade the connection", err)
	}
	defer logs.CloseAndLogErr(websocketConn)

	stdinReader, stdinWriter := io.Pipe()
	defer logs.CloseAndLogErr(stdinWriter)
	stdoutReader, stdoutWriter := io.Pipe()
	defer logs.CloseAndLogErr(stdoutWriter)

	errorChan := make(chan error, 3)

	sizeQueue := kubecli.NewTerminalSizeQueue()
	defer sizeQueue.Close()
	go ws.StreamFromWebsocketToWriter(websocketConn, stdinWriter, errorChan, ws.ResizeHandler(sizeQueue))
	go ws.StreamFromReaderToWebsocket(websocketConn, stdoutReader, errorChan)
	go func() {
		err := handler.kubeClient.StartExecProcess(kubernetes.ExecProcessParams{
			Token:         token,
			Namespace:     namespace,
			PodName:       podName,
			ContainerName: containerName,
			Command:       commandArray,
			Stdin:         stdinReader,
			Stdout:        stdoutWriter,
			ResizeQueue:   sizeQueue,
		})
		errorChan <- err
	}()

	err = <-errorChan

	if err == nil || errors.Is(err, io.EOF) {
		// exec process ended normally (shell exited) - send a clean close frame to the browser
		_ = websocketConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

		return nil
	}

	if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		log.Error().Err(err).Msg("websocket error")

		return nil
	}

	return httperror.InternalServerError("Unable to start exec process inside container", err)
}
