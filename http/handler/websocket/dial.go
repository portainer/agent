// +build !windows

package websocket

import (
	"net"
)

func createDial() (net.Conn, error) {
	return net.Dial("unix", "/var/run/docker.sock")
}
