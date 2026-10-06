//go:build windows

package setup

import (
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// gateways lists the default gateways (and DNS servers, which on campus
// networks are often the firewall itself) of every adapter that is up.
// Uses the API directly so no console window flashes.
func gateways() []net.IP {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST
	size := uint32(15 << 10)
	var buf []byte
	for i := 0; i < 3; i++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil
		}
		buf = nil
	}
	if buf == nil {
		return nil
	}
	var gws, dns []net.IP
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp {
			continue
		}
		for g := a.FirstGatewayAddress; g != nil; g = g.Next {
			gws = append(gws, g.Address.IP())
		}
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			dns = append(dns, d.Address.IP())
		}
	}
	return append(gws, dns...)
}
