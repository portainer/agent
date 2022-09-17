package net

import (
	"net"

	"github.com/rs/zerolog/log"
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

		log.Debug().Str("host", host).Int("result", idx+1).Str("ip", ip.String()).Msg("")
	}

	return ipAddresses, nil
}
