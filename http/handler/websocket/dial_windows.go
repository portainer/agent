// +build windows

package websocket

import (
	"net"
)

func createDial() (net.Conn, error) {
	return winio.DialPipe("//./pipe/docker_engine", nil)
}
