package config

import (
	"errors"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// net.Interfaces followed by Interface.Addrs repeats GetAdaptersAddresses for
// every adapter on Windows. One live enumeration preserves the same selection
// rules without caching network state or delaying address changes.
func lanInterfaceAddresses() ([]net.Addr, error) {
	size := uint32(15_000)
	for {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_PREFIX, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) && size > uint32(len(buffer)) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var addresses []net.Addr
		if size == 0 {
			return addresses, nil
		}
		for adapter := first; adapter != nil; adapter = adapter.Next {
			if adapter.OperStatus != windows.IfOperStatusUp || adapter.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
				continue
			}
			for unicast := adapter.FirstUnicastAddress; unicast != nil; unicast = unicast.Next {
				address, err := unicast.Address.Sockaddr.Sockaddr()
				if err != nil {
					return nil, err
				}
				if ipv4, ok := address.(*syscall.SockaddrInet4); ok {
					addresses = append(addresses, &net.IPNet{
						IP:   net.IPv4(ipv4.Addr[0], ipv4.Addr[1], ipv4.Addr[2], ipv4.Addr[3]),
						Mask: net.CIDRMask(int(unicast.OnLinkPrefixLength), 32),
					})
				}
			}
		}
		return addresses, nil
	}
}
