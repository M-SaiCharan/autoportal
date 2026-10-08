// Package update keeps autoportal current from its GitHub releases.
//
// It deliberately avoids api.github.com: unauthenticated API calls are
// limited to 60 per hour per IP address, and a whole campus shares one
// address. The latest tag is read from the redirect of
// github.com/<repo>/releases/latest instead, and files come from the
// release download URLs. Every download is checked against the release's
// SHA256SUMS.txt before it replaces anything.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from.
const Repo = "M-SaiCharan/autoportal"

// Asset overrides the release file this build updates from. The Windows
// console build sets it with -ldflags, because it can't be told apart
// from the GUI build at run time.
var Asset string

// maxDownload bounds a download; releases are around 10 MB.
const maxDownload = 64 << 20

// ErrDevBuild means this binary was not built from a release tag, so it
// is never replaced automatically.
var ErrDevBuild = errors.New("this is a development build; automatic updates are off")

// Updater checks for and installs new releases.
type Updater struct {
	Current string // running version, e.g. "v0.2.0"
	Exe     string // path of the running executable (resolved at start-up)

	BaseURL string       // default $AUTOPORTAL_UPDATE_BASE, then https://github.com
	Repo    string       // default Repo
	Client  *http.Client // default: 2-minute timeout, no automatic redirects
}

func (u *Updater) base() string {
	b := u.BaseURL
	if b == "" {
		b = os.Getenv("AUTOPORTAL_UPDATE_BASE") // for testing against a local server
	}
	if b == "" {
		b = "https://github.com"
	}
	r := u.Repo
	if r == "" {
		r = Repo
	}
	return strings.TrimRight(b, "/") + "/" + r
}

func (u *Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// Latest returns the newest release tag.
func (u *Updater) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.base()+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	c := *u.client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode/100 != 3 || loc == "" {
		return "", fmt.Errorf("checking for updates: unexpected response %s", resp.Status)
	}
	tag := path.Base(loc) // …/releases/tag/v1.2.3
	if _, ok := parseVersion(tag); !ok || !strings.Contains(loc, "/releases/tag/") {
		return "", fmt.Errorf("checking for updates: unexpected redirect to %s", loc)
	}
	return tag, nil
}

// Check returns the newer release tag, or "" when up to date.
func (u *Updater) Check(ctx context.Context) (string, error) {
	cur, ok := parseVersion(u.Current)
	if !ok {
		return "", ErrDevBuild
	}
	tag, err := u.Latest(ctx)
	if err != nil {
		return "", err
	}
	v, _ := parseVersion(tag)
	if !v.newer(cur) {
		return "", nil
	}
	return tag, nil
}

// Supported reports whether this executable can update itself, and if
// not, why.
func (u *Updater) Supported() error {
	if _, ok := parseVersion(u.Current); !ok {
		return ErrDevBuild
	}
	if _, err := u.asset(); err != nil {
		return err
	}
	if strings.Contains(u.Exe, "/AppTranslocation/") {
		return errors.New("macOS is running autoportal from a temporary location; move autoportal.app into Applications")
	}
	return nil
}

// asset is the release file for this platform.
func (u *Updater) asset() (string, error) {
	if Asset != "" {
		return Asset, nil
	}
	switch runtime.GOOS {
	case "linux":
		return "autoportal-linux-" + runtime.GOARCH, nil
	case "windows":
		return "autoportal-windows-" + runtime.GOARCH + ".exe", nil
	case "darwin":
		if bundle(u.Exe) == "" {
			return "", errors.New("automatic updates on macOS need autoportal.app")
		}
		return "autoportal-macos.zip", nil
	}
	return "", fmt.Errorf("no release builds for %s", runtime.GOOS)
}

// Apply downloads release tag, verifies it, and replaces the running
// executable (or, on macOS, the whole app bundle). The new version runs
// after Restart.
func (u *Updater) Apply(ctx context.Context, tag string) error {
	if err := u.Supported(); err != nil {
		return err
	}
	name, _ := u.asset()
	sums, err := u.fetch(ctx, tag, "SHA256SUMS.txt", 1<<20)
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return err
	}
	data, err := u.fetch(ctx, tag, name, maxDownload)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("downloaded %s does not match its checksum; not installing it", name)
	}
	if runtime.GOOS == "darwin" {
		return replaceBundle(bundle(u.Exe), data)
	}
	return replaceFile(u.Exe, data)
}

func (u *Updater) fetch(ctx context.Context, tag, name string, limit int64) ([]byte, error) {
	url := u.base() + "/releases/download/" + tag + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("downloading %s: file is too large", name)
	}
	return b, nil
}

// checksumFor finds name in a sha256sum listing.
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("the release has no checksum for %s", name)
}

// replaceFile swaps exe for data. On Windows a running .exe can't be
// overwritten but can be renamed, so the old one is moved aside first and
// deleted on the next start (see Cleanup).
func replaceFile(exe string, data []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".autoportal-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(tmp.Name(), exe)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

// Cleanup removes what a previous update left behind.
func Cleanup(exe string) {
	_ = os.Remove(exe + ".old")
}

// bundle returns the .app folder exe lives in, or "".
func bundle(exe string) string {
	const marker = ".app/Contents/MacOS/"
	i := strings.LastIndex(exe, marker)
	if i < 0 {
		return ""
	}
	return exe[:i+len(".app")]
}

// version is a parsed vMAJOR.MINOR.PATCH.
type version [3]int

func parseVersion(s string) (version, bool) {
	var v version
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if !strings.HasPrefix(s, "v") || len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func (v version) newer(than version) bool {
	for i := range v {
		if v[i] != than[i] {
			return v[i] > than[i]
		}
	}
	return false
}
