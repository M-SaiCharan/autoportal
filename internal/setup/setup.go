// Package setup holds the steps shared by the terminal and graphical
// setup: find the portal, pin its certificate, verify the credentials and
// save everything.
package setup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/M-SaiCharan/autoportal/internal/portal"
	"github.com/M-SaiCharan/autoportal/internal/store"
)

// Portal is a portal that has been contacted and identified.
type Portal struct {
	Base        *url.URL
	Fingerprint string // empty for plain-HTTP portals
	CertSubject string
	Info        portal.Info
}

// Endpoint returns the pinned endpoint.
func (p *Portal) Endpoint() *portal.Endpoint {
	return &portal.Endpoint{Base: p.Base, Fingerprint: p.Fingerprint}
}

// Inspect contacts the portal at raw (any form ParseEndpoint accepts),
// records its certificate and identifies its type. If a bare address
// doesn't answer on HTTPS, plain HTTP on the same port is tried.
func Inspect(ctx context.Context, raw string) (*Portal, error) {
	base, err := portal.ParseEndpoint(raw)
	if err != nil {
		return nil, err
	}
	p, err := inspect(ctx, base)
	if err != nil && base.Scheme == "https" && !strings.Contains(raw, "://") {
		alt := *base
		alt.Scheme = "http"
		if p2, err2 := inspect(ctx, &alt); err2 == nil {
			return p2, nil
		}
	}
	return p, err
}

func inspect(ctx context.Context, base *url.URL) (*Portal, error) {
	p := &Portal{Base: base}
	if base.Scheme == "https" {
		cert, err := portal.FetchCertificate(ctx, base)
		if err != nil {
			return nil, fmt.Errorf("cannot reach %s: %w", base.Host, err)
		}
		p.Fingerprint = portal.Fingerprint(cert)
		p.CertSubject = cert.Subject.CommonName
	}
	info, err := portal.Detect(ctx, p.Endpoint())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", base.Host, err)
	}
	p.Info = info
	return p, nil
}

// Discover looks for a portal at the usual suspects (the default
// gateway) and returns the first one found.
func Discover(ctx context.Context) (*Portal, error) {
	for _, c := range Candidates() {
		cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		p, err := Inspect(cctx, c)
		cancel()
		if err == nil {
			return p, nil
		}
	}
	return nil, errors.New("no login portal found automatically")
}

// Candidates returns host addresses that commonly run the captive portal.
func Candidates() []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range gateways() {
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		if s := ip.String(); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Verify logs in with the given credentials. Success means the
// credentials work (and the user is now online).
func Verify(ctx context.Context, p *Portal, username, password string) (portal.Result, error) {
	a, err := portal.Open(p.Info.Driver, p.Endpoint())
	if err != nil {
		return portal.Result{}, err
	}
	return a.Login(ctx, username, password)
}

// Save stores the portal and credentials. prev (may be nil) supplies
// settings to keep; a password saved under a different account is removed.
func Save(p *Portal, username, password string, prev *store.Config) (*store.Config, error) {
	c := &store.Config{}
	if prev != nil {
		*c = *prev
	}
	c.Portal = store.PortalConfig{
		Type:        p.Info.Driver,
		URL:         p.Base.String(),
		Fingerprint: p.Fingerprint,
		Title:       p.Info.Title,
	}
	c.Username = username
	c.Paused = false
	if err := store.SetPassword(c, password); err != nil {
		return nil, err
	}
	if err := store.Save(c); err != nil {
		return nil, err
	}
	if prev != nil && prev.Account() != c.Account() {
		_ = store.DeletePassword(prev)
	}
	return c, nil
}

// Friendly turns common errors into advice for non-technical users.
func Friendly(err error) string {
	var mm *portal.CertMismatchError
	var ne net.Error
	switch {
	case err == nil:
		return ""
	case portal.IsRejected(err):
		return "The portal rejected the username or password."
	case errors.Is(err, portal.ErrChallenge):
		return "The portal asked for an extra verification step, which autoportal can't do automatically."
	case errors.As(err, &mm):
		return "The portal's security certificate changed. If your college renewed it, run setup again."
	case errors.As(err, &ne) && ne.Timeout():
		return "The portal didn't answer. Make sure you are connected to the campus network."
	}
	msg := err.Error()
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "i/o timeout") {
		return "The portal can't be reached. Make sure you are connected to the campus network."
	}
	return msg
}
