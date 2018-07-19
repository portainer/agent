// +build windows

package websocket

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func createDial() (net.Conn, error) {
	return winio.DialPipe("//./pipe/docker_engine", nil)
}
