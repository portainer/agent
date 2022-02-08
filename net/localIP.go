package net

import (
	"errors"
	"net"
)

// GetLocalIP is used to retrieve the first non loop-back local IP address.
func GetLocalIP() (ip string, err error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = ipnet.IP.String()
				return
			}
		}
	}

	err = errors.New("unable to retrieve the local IP")
	return
}
