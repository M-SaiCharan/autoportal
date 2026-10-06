//go:build darwin

package setup

import (
	"net"
	"os/exec"
	"strings"
)

// gateways asks the routing table for the default route.
func gateways() []net.IP {
	out, err := exec.Command("/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && k == "gateway" {
			if ip := net.ParseIP(strings.TrimSpace(v)); ip != nil {
				return []net.IP{ip}
			}
		}
	}
	return nil
}
