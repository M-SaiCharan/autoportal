//go:build linux

package setup

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"
)

// gateways reads default routes from /proc/net/route, where addresses are
// little-endian hex.
func gateways() []net.IP {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer f.Close()
	return parseProcRoute(bufio.NewScanner(f))
}

func parseProcRoute(sc *bufio.Scanner) []net.IP {
	var out []net.IP
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fields[2])
		if err != nil || len(b) != 4 {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, binary.LittleEndian.Uint32(b))
		out = append(out, ip)
	}
	return out
}
