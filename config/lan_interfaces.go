//go:build !windows

package config

import "net"

func lanInterfaceAddresses() ([]net.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addrs []net.Addr
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if addresses, err := iface.Addrs(); err == nil {
			addrs = append(addrs, addresses...)
		}
	}
	return addrs, nil
}
