// Package portal defines the interface every captive-portal adapter
// implements, plus a small registry so new firewall brands can be added
// without touching the agent.
package portal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Result is what a portal said in response to a login or logout.
type Result struct {
	// Message is the human-readable text from the portal, already
	// HTML-unescaped and with placeholders filled in.
	Message string
}

// ErrChallenge means the portal asked for an extra step (e.g. an OTP)
// that cannot be completed automatically.
var ErrChallenge = errors.New("portal requested an additional verification step")

// RejectedError means the portal refused the credentials (wrong password,
// expired account, login limit...). Retrying automatically is pointless and
// may lock the account, so callers must stop until the user acts.
type RejectedError struct {
	Message string
}

func (e *RejectedError) Error() string {
	if e.Message == "" {
		return "login rejected by portal"
	}
	return "login rejected: " + e.Message
}

// IsRejected reports whether err is (or wraps) a *RejectedError.
func IsRejected(err error) bool {
	var r *RejectedError
	return errors.As(err, &r)
}

// Adapter talks to one specific captive portal.
type Adapter interface {
	// Reachable checks, without sending credentials, that the portal
	// is reachable and (for HTTPS) presents the pinned certificate.
	Reachable(ctx context.Context) error
	// Login signs the user in. Logging in while already signed in must be
	// harmless.
	Login(ctx context.Context, username, password string) (Result, error)
	// Logout signs the user out.
	Logout(ctx context.Context, username string) (Result, error)
}

// Info is what Detect learned about a portal.
type Info struct {
	Driver string // driver name, e.g. "sophos"
	Title  string // portal's own title, e.g. "Sign in to access the X network"
}

// Driver knows how to recognise and talk to one kind of portal.
type Driver struct {
	Name string
	// Detect reports whether the portal at ep is of this kind.
	Detect func(ctx context.Context, ep *Endpoint) (Info, bool, error)
	// New returns an adapter for the portal at ep.
	New func(ep *Endpoint) Adapter
}

var (
	mu      sync.RWMutex
	drivers = map[string]Driver{}
)

// Register makes a driver available. It is meant to be called from init.
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := drivers[d.Name]; dup {
		panic("portal: driver registered twice: " + d.Name)
	}
	drivers[d.Name] = d
}

// Open returns an adapter for the named driver.
func Open(name string, ep *Endpoint) (Adapter, error) {
	mu.RLock()
	d, ok := drivers[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown portal type %q", name)
	}
	return d.New(ep), nil
}

// Detect asks every registered driver whether it recognises the portal.
func Detect(ctx context.Context, ep *Endpoint) (Info, error) {
	mu.RLock()
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	mu.RUnlock()
	sort.Strings(names)

	var firstErr error
	for _, n := range names {
		mu.RLock()
		d := drivers[n]
		mu.RUnlock()
		info, ok, err := d.Detect(ctx, ep)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok {
			info.Driver = d.Name
			return info, nil
		}
	}
	if firstErr != nil {
		return Info{}, firstErr
	}
	return Info{}, errors.New("this does not look like a supported login portal")
}
