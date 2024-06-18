package net

import (
	"errors"
	"net"
)

var ErrNoLocalIP = errors.New("unable to retrieve the local IP address")

// GetLocalIP is used to retrieve the first non loop-back local IP address.
func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", ErrNoLocalIP
}
