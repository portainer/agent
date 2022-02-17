package net

import (
	"log"
	"net"
)

// LookupIPAddresses returns a slice of IPv4 and IPv6 addresses associated to the host
// On error, it returns an empty slice and the error.
func LookupIPAddresses(host string) ([]string, error) {
	ipAddresses := make([]string, 0)

	ips, err := net.LookupIP(host)
	if err != nil {
		return ipAddresses, err
	}

	for idx, ip := range ips {
		ipAddresses = append(ipAddresses, ip.String())
		log.Printf("[DEBUG] [net] [host: %s] [result: %d] [ip: %s]", host, idx+1, ip.String())
	}

	return ipAddresses, nil
}
