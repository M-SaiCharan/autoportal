package netwatch

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
)

// Event says why a re-check is worthwhile.
type Event struct {
	Reason string // "wake" or "network-change"
}

// Watcher polls cheaply (no network traffic) for two signals:
//
//   - wake from sleep: the wall clock jumped much further than the poll
//     interval, which only happens if the machine was suspended;
//   - network change: the set of local interface addresses changed
//     (cable plugged in, Wi-Fi joined, new DHCP lease, ...).
//
// Polling works identically on Linux, macOS and Windows, which keeps the
// tool free of per-OS event plumbing.
type Watcher struct {
	Interval time.Duration
	// Addrs returns a fingerprint of the current network configuration.
	Addrs func() string
	// Now returns the current wall-clock time.
	Now func() time.Time
}

// NewWatcher returns a watcher with production defaults.
func NewWatcher() *Watcher {
	return &Watcher{Interval: 3 * time.Second, Addrs: InterfaceSignature, Now: time.Now}
}

// Watch sends events until ctx is done. The channel is closed on exit.
func (w *Watcher) Watch(ctx context.Context) <-chan Event {
	out := make(chan Event, 1)
	go func() {
		defer close(out)
		t := time.NewTicker(w.Interval)
		defer t.Stop()
		// Round(0) strips the monotonic reading so Sub measures wall time,
		// which (unlike the monotonic clock on some OSes) includes sleep.
		last := w.Now().Round(0)
		sig := w.Addrs()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			now := w.Now().Round(0)
			gap := now.Sub(last)
			last = now
			if ev, ok := w.step(gap, &sig); ok {
				select {
				case out <- ev:
				default: // a check is already pending; that one will do
				}
			}
		}
	}()
	return out
}

// step evaluates one tick; split out for testing.
func (w *Watcher) step(gap time.Duration, sig *string) (Event, bool) {
	cur := w.Addrs()
	changed := cur != *sig
	*sig = cur
	if gap > 3*w.Interval+10*time.Second {
		return Event{Reason: "wake"}, true
	}
	if changed {
		return Event{Reason: "network-change"}, true
	}
	return Event{}, false
}

// InterfaceSignature returns a stable string describing every up,
// non-loopback interface and its addresses.
func InterfaceSignature() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "error"
	}
	var parts []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.IsLinkLocalUnicast() {
				continue // link-local addresses come and go without meaning
			}
			parts = append(parts, ifc.Name+"="+ip.String())
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
