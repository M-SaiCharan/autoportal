//go:build linux

package setup

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseProcRoute(t *testing.T) {
	const table = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
wlp2s0	00000000	020A0A0A	0003	0	0	600	00000000	0	0	0
wlp2s0	000A0A0A	00000000	0001	0	0	600	00FFFFFF	0	0	0
`
	ips := parseProcRoute(bufio.NewScanner(strings.NewReader(table)))
	if len(ips) != 1 || ips[0].String() != "10.10.10.2" {
		t.Fatalf("got %v, want [10.10.10.2]", ips)
	}
}
