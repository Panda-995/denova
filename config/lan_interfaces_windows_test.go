package config

import (
	"net"
	"testing"
)

func TestLANAddressMatchesActiveWindowsInterfaces(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var expected []net.Addr
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, addresses...)
	}
	addresses, err := lanInterfaceAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := selectLANAddress(addresses), selectLANAddress(expected); got != want {
		t.Fatalf("LAN address=%s, active interface selection=%s", got, want)
	}
}
