// Package sophos implements the Sophos Firewall (SFOS) / Cyberoam
// captive-portal protocol, as used by the portal's own httpclient.js:
//
//	POST login.xml   mode=191&username=..&password=..&a=<ms>&producttype=0
//	POST logout.xml  mode=193&username=..&a=<ms>&producttype=0
//
// Both answer with <requestresponse><status>LIVE|LOGIN|CHALLENGE</status>
// <message>..</message><state>..</state></requestresponse>.
package sophos

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/M-SaiCharan/autoportal/internal/portal"
)

const (
	modeLogin  = "191"
	modeLogout = "193"

	statusLive      = "LIVE"
	statusLogin     = "LOGIN"
	statusChallenge = "CHALLENGE"

	// producttype 0 = web browser client (1 = iOS, 2 = Android).
	productWeb = "0"

	requestTimeout = 8 * time.Second
	maxBody        = 64 << 10
)

func init() {
	portal.Register(portal.Driver{
		Name:   "sophos",
		Detect: Detect,
		New:    func(ep *portal.Endpoint) portal.Adapter { return New(ep) },
	})
}

// Client is a Sophos captive-portal client.
type Client struct {
	ep  *portal.Endpoint
	now func() time.Time
}

// New returns a client for the portal at ep.
func New(ep *portal.Endpoint) *Client {
	return &Client{ep: ep, now: time.Now}
}

type response struct {
	Status  string `xml:"status"`
	Message string `xml:"message"`
	State   string `xml:"state"`
}

// Reachable implements portal.Adapter.
func (c *Client) Reachable(ctx context.Context) error { return c.ep.Reachable(ctx) }

// Login implements portal.Adapter.
func (c *Client) Login(ctx context.Context, username, password string) (portal.Result, error) {
	form := url.Values{
		"mode": {modeLogin},
		// The portal's JS doubles single quotes before sending; match it.
		"username":    {strings.ReplaceAll(username, "'", "''")},
		"password":    {password},
		"a":           {c.timestamp()},
		"producttype": {productWeb},
	}
	r, err := c.post(ctx, "login.xml", form)
	if err != nil {
		return portal.Result{}, err
	}
	res := portal.Result{Message: cleanMessage(r.Message, username)}
	switch r.Status {
	case statusLive:
		return res, nil
	case statusLogin:
		return res, &portal.RejectedError{Message: res.Message}
	case statusChallenge:
		return res, fmt.Errorf("%w: %s", portal.ErrChallenge, res.Message)
	default:
		return res, fmt.Errorf("unexpected portal status %q", r.Status)
	}
}

// Logout implements portal.Adapter.
func (c *Client) Logout(ctx context.Context, username string) (portal.Result, error) {
	form := url.Values{
		"mode":        {modeLogout},
		"username":    {strings.ReplaceAll(username, "'", "''")},
		"a":           {c.timestamp()},
		"producttype": {productWeb},
	}
	r, err := c.post(ctx, "logout.xml", form)
	if err != nil {
		return portal.Result{}, err
	}
	res := portal.Result{Message: cleanMessage(r.Message, username)}
	if r.Status != statusLogin {
		return res, fmt.Errorf("logout not confirmed (status %q)", r.Status)
	}
	return res, nil
}

func (c *Client) timestamp() string {
	return strconv.FormatInt(c.now().UnixMilli(), 10)
}

func (c *Client) post(ctx context.Context, path string, form url.Values) (*response, error) {
	hc, err := c.ep.HTTPClient(requestTimeout)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep.URL(path), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("portal answered HTTP %d", resp.StatusCode)
	}
	var r response
	if err := xml.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("unreadable portal response: %w", err)
	}
	r.Status = strings.TrimSpace(r.Status)
	return &r, nil
}

// cleanMessage turns "You&#39;ve signed out" / "signed in as {username}"
// into display text.
func cleanMessage(msg, username string) string {
	msg = html.UnescapeString(strings.TrimSpace(msg))
	return strings.ReplaceAll(msg, "{username}", username)
}

var titleRE = regexp.MustCompile(`var\s+title\s*=\s*'([^']*)'`)

// Detect implements portal.Driver.Detect by fetching httpclient.html and
// looking for the Sophos/Cyberoam client scripts.
func Detect(ctx context.Context, ep *portal.Endpoint) (portal.Info, bool, error) {
	hc, err := ep.HTTPClient(requestTimeout)
	if err != nil {
		return portal.Info{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.URL("httpclient.html"), nil)
	if err != nil {
		return portal.Info{}, false, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return portal.Info{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return portal.Info{}, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return portal.Info{}, false, err
	}
	page := string(body)
	if !strings.Contains(page, "cyberoamAjax.js") && !strings.Contains(page, "/validation/httpclient.js") {
		return portal.Info{}, false, nil
	}
	info := portal.Info{Title: "Sophos captive portal"}
	if m := titleRE.FindStringSubmatch(page); m != nil && strings.TrimSpace(m[1]) != "" {
		info.Title = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	return info, true, nil
}
