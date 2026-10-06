// Package netwatch answers "is the internet reachable?" and notices the
// moments worth re-checking: wake from sleep and network changes.
package netwatch

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Connectivity is the outcome of a probe.
type Connectivity int

const (
	// Offline: no probe got any HTTP answer (no network, DNS failure, timeout).
	Offline Connectivity = iota
	// Blocked: something answered but not with the expected content,
	// typically a captive portal or its proxy (Sophos replies 403).
	Blocked
	// Online: the open internet is reachable.
	Online
)

func (c Connectivity) String() string {
	switch c {
	case Online:
		return "online"
	case Blocked:
		return "blocked"
	default:
		return "offline"
	}
}

// Target is one well-known connectivity-check URL.
type Target struct {
	URL      string
	Status   int    // expected HTTP status
	Contains string // expected body substring; empty = don't check
}

// DefaultTargets are the URLs operating systems themselves use for captive
// portal detection. They are plain HTTP on purpose: captive portals
// intercept HTTP, while HTTPS just fails.
var DefaultTargets = []Target{
	{URL: "http://connectivitycheck.gstatic.com/generate_204", Status: http.StatusNoContent},
	{URL: "http://www.msftconnecttest.com/connecttest.txt", Status: http.StatusOK, Contains: "Microsoft Connect Test"},
	{URL: "http://captive.apple.com/hotspot-detect.html", Status: http.StatusOK, Contains: "Success"},
}

// Prober checks internet connectivity.
type Prober struct {
	Targets []Target
	Timeout time.Duration

	once   sync.Once
	client *http.Client
}

// NewProber returns a prober using DefaultTargets.
func NewProber() *Prober {
	return &Prober{Targets: DefaultTargets, Timeout: 4 * time.Second}
}

func (p *Prober) httpClient() *http.Client {
	p.once.Do(func() {
		p.client = &http.Client{
			Transport: &http.Transport{
				Proxy:             nil, // probe the network itself, not a configured proxy
				DialContext:       (&net.Dialer{Timeout: p.Timeout}).DialContext,
				DisableKeepAlives: true,
			},
			// A redirect is itself the captive-portal signal; never follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	})
	return p.client
}

// Probe queries all targets in parallel. It returns Online as soon as one
// target answers correctly, Blocked if something answered wrongly, and
// Offline if nothing answered at all.
func (p *Prober) Probe(ctx context.Context) Connectivity {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	results := make(chan Connectivity, len(p.Targets))
	for _, t := range p.Targets {
		go func(t Target) { results <- p.check(ctx, t) }(t)
	}
	best := Offline
	for range p.Targets {
		switch r := <-results; r {
		case Online:
			return Online
		case Blocked:
			best = Blocked
		}
	}
	return best
}

func (p *Prober) check(ctx context.Context, t Target) Connectivity {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return Offline
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return Offline
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != t.Status {
		return Blocked
	}
	if t.Contains != "" && !strings.Contains(string(body), t.Contains) {
		return Blocked
	}
	return Online
}
