package portal

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultPort is used when the user types a bare host. 8090 is the
// Sophos/Cyberoam captive-portal port.
const DefaultPort = "8090"

// CertMismatchError means the portal presented a different certificate
// than the one pinned at setup. That is either a legitimate renewal or
// someone impersonating the portal, so credentials are never sent.
type CertMismatchError struct {
	Want, Got string
}

func (e *CertMismatchError) Error() string {
	return fmt.Sprintf("portal certificate changed (expected %s, got %s)", e.Want, e.Got)
}

// ErrNotPinned is returned when an HTTPS endpoint has no pinned certificate.
var ErrNotPinned = errors.New("portal certificate is not pinned; run setup")

// Endpoint is a portal base URL plus the pinned certificate fingerprint.
type Endpoint struct {
	Base        *url.URL // scheme://host:port, no path
	Fingerprint string   // canonical SHA-256 fingerprint; empty for plain HTTP
}

// ParseEndpoint turns user input such as "10.0.0.1",
// "10.0.0.1:8090" or "https://10.0.0.1:8090/httpclient.html" into a base
// URL. A bare host gets https and port 8090.
func ParseEndpoint(raw string) (*url.URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("portal address is empty")
	}
	bare := !strings.Contains(s, "://")
	if bare {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid portal address: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("portal address must start with http:// or https://")
	}
	if u.Hostname() == "" {
		return nil, errors.New("portal address has no host")
	}
	host := u.Host
	if u.Port() == "" && bare {
		host = net.JoinHostPort(u.Hostname(), DefaultPort)
	}
	return &url.URL{Scheme: u.Scheme, Host: host}, nil
}

// NormalizeFingerprint accepts a SHA-256 fingerprint in common spellings
// ("AB:CD:..", "abcd..", "sha256 Fingerprint=AB:CD..") and returns the
// canonical upper-case, colon-separated form.
func NormalizeFingerprint(s string) (string, error) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "="); i >= 0 {
		s = s[i+1:]
	}
	s = strings.NewReplacer(":", "", " ", "", "-", "").Replace(s)
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != sha256.Size {
		return "", errors.New("not a SHA-256 fingerprint")
	}
	return formatFingerprint(b), nil
}

func formatFingerprint(b []byte) string {
	h := strings.ToUpper(hex.EncodeToString(b))
	var sb strings.Builder
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(h[i : i+2])
	}
	return sb.String()
}

// Fingerprint returns the canonical SHA-256 fingerprint of a certificate.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return formatFingerprint(sum[:])
}

// FetchCertificate connects to an HTTPS portal and returns its leaf
// certificate without verifying it. It is used once, at setup, to pin
// the certificate (trust on first use).
func FetchCertificate(ctx context.Context, base *url.URL) (*x509.Certificate, error) {
	if base.Scheme != "https" {
		return nil, errors.New("portal does not use HTTPS")
	}
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		Config:    &tls.Config{InsecureSkipVerify: true, ServerName: base.Hostname()},
	}
	conn, err := d.DialContext(ctx, "tcp", base.Host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("portal presented no certificate")
	}
	return certs[0], nil
}

// tlsConfig returns a TLS config that accepts exactly the pinned
// certificate. Captive portals almost always use self-signed certificates,
// so normal CA verification would fail; pinning is stricter than CA
// verification anyway.
func (e *Endpoint) tlsConfig() (*tls.Config, error) {
	if e.Fingerprint == "" {
		return nil, ErrNotPinned
	}
	want := e.Fingerprint
	return &tls.Config{
		InsecureSkipVerify: true, // replaced by the pin check below
		ServerName:         e.Base.Hostname(),
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("portal presented no certificate")
			}
			if got := Fingerprint(cs.PeerCertificates[0]); got != want {
				return &CertMismatchError{Want: want, Got: got}
			}
			return nil
		},
	}, nil
}

// HTTPClient returns a client that talks only to this portal: no proxy,
// no redirects, and certificate pinning for HTTPS.
func (e *Endpoint) HTTPClient(timeout time.Duration) (*http.Client, error) {
	tr := &http.Transport{
		Proxy:               nil, // the portal is on the local network
		DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
		TLSHandshakeTimeout: timeout,
		MaxIdleConns:        1,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   true, // requests are rare; don't hold sockets open
	}
	if e.Base.Scheme == "https" {
		cfg, err := e.tlsConfig()
		if err != nil {
			return nil, err
		}
		tr.TLSClientConfig = cfg
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// Reachable opens a connection to the portal (and completes the pinned
// TLS handshake for HTTPS) without sending any HTTP request.
func (e *Endpoint) Reachable(ctx context.Context) error {
	nd := &net.Dialer{Timeout: 3 * time.Second}
	if e.Base.Scheme != "https" {
		c, err := nd.DialContext(ctx, "tcp", e.Base.Host)
		if err != nil {
			return err
		}
		return c.Close()
	}
	cfg, err := e.tlsConfig()
	if err != nil {
		return err
	}
	d := tls.Dialer{NetDialer: nd, Config: cfg}
	c, err := d.DialContext(ctx, "tcp", e.Base.Host)
	if err != nil {
		// Surface the pin failure itself rather than a wrapped TLS alert.
		var mm *CertMismatchError
		if errors.As(err, &mm) {
			return mm
		}
		return err
	}
	return c.Close()
}

// URL resolves a path relative to the portal base.
func (e *Endpoint) URL(path string) string {
	return e.Base.JoinPath(path).String()
}
