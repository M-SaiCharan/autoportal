// Package doctor checks every piece autoportal depends on and writes a
// plain-text report a student can paste to whoever helps them. The report
// never contains the password.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/M-SaiCharan/autoportal/internal/agent"
	"github.com/M-SaiCharan/autoportal/internal/autostart"
	"github.com/M-SaiCharan/autoportal/internal/history"
	"github.com/M-SaiCharan/autoportal/internal/instance"
	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	"github.com/M-SaiCharan/autoportal/internal/portal"
	"github.com/M-SaiCharan/autoportal/internal/setup"
	"github.com/M-SaiCharan/autoportal/internal/store"
	"github.com/M-SaiCharan/autoportal/internal/update"
)

// Status of one check.
type Status int

const (
	OK Status = iota
	Info
	Warn
	Fail
)

func (s Status) String() string {
	return [...]string{" OK ", "INFO", "WARN", "FAIL"}[s]
}

// Check is one line of the report.
type Check struct {
	Status Status
	Name   string
	Detail string
	Hint   string // what to do about it; empty when nothing
}

// Inputs is what the caller knows that the checks can't find out.
type Inputs struct {
	Version string
	Exe     string
	LogPath string
	History *history.History // may be nil
	State   *agent.State     // the running agent's state, when called from the app
	InApp   bool             // called from inside the running app
	Prober  interface {
		Probe(context.Context) netwatch.Connectivity
	}
	Updater  *update.Updater // nil skips the update check
	LogLines int             // default 40
	Now      func() time.Time
}

// Report is the result of Run.
type Report struct {
	When    time.Time
	Header  []string
	Checks  []Check
	LogTail []string
}

// Run performs all checks. It takes a few seconds at most.
func Run(ctx context.Context, in Inputs) *Report {
	if in.Now == nil {
		in.Now = time.Now
	}
	if in.Prober == nil {
		in.Prober = netwatch.NewProber()
	}
	if in.LogLines == 0 {
		in.LogLines = 40
	}
	r := &Report{When: in.Now()}
	r.Header = []string{
		"autoportal " + in.Version + " on " + osName() + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")",
		"program: " + in.Exe,
	}

	cfg, err := store.Load()
	switch {
	case errors.Is(err, store.ErrNotConfigured):
		r.add(Fail, "Settings", "not set up yet", "Open the autoportal menu and choose Set up…")
	case err != nil:
		r.add(Fail, "Settings", err.Error(), "Run setup again to rewrite the settings file.")
	default:
		r.add(OK, "Settings", fmt.Sprintf("portal %s (%s), user %s", cfg.Portal.URL, cfg.Portal.Title, cfg.Username), "")
	}

	// The slow network checks run in parallel.
	var (
		wg       sync.WaitGroup
		conn     netwatch.Connectivity
		reachErr error
		latest   string
		latErr   error
	)
	wg.Add(1)
	go func() { defer wg.Done(); conn = in.Prober.Probe(ctx) }()
	if cfg != nil {
		if ad, err := cfg.Adapter(); err != nil {
			reachErr = err
		} else {
			wg.Add(1)
			go func() { defer wg.Done(); reachErr = ad.Reachable(ctx) }()
		}
	}
	if in.Updater != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			latest, latErr = in.Updater.Latest(c)
		}()
	}
	wg.Wait()

	if ifs := interfaces(); len(ifs) == 0 {
		r.add(Fail, "Network", "no network connection", "Connect to the campus Wi-Fi or plug in the Ethernet cable.")
	} else {
		r.add(OK, "Network", strings.Join(ifs, ", "), "")
	}

	if cfg != nil {
		var mm *portal.CertMismatchError
		switch {
		case errors.As(reachErr, &mm):
			r.add(Fail, "Portal", "certificate changed: now "+mm.Got,
				"If you are on campus and your college renewed its certificate, run Set up again.")
		case reachErr != nil:
			r.add(Warn, "Portal", "not reachable: "+setup.Friendly(reachErr),
				"Normal when you are away from campus. On campus, check the portal address in Set up.")
		default:
			detail := "reachable"
			if cfg.Portal.Fingerprint != "" {
				detail += ", certificate matches"
			}
			r.add(OK, "Portal", detail, "")
		}
	}

	switch conn {
	case netwatch.Online:
		r.add(OK, "Internet", "working", "")
	case netwatch.Blocked:
		r.add(Warn, "Internet", "blocked (not signed in to the portal yet)", "Choose Log in now from the menu.")
	default:
		r.add(Warn, "Internet", "not reachable", "")
	}

	if cfg != nil {
		if _, err := store.GetPassword(cfg); err != nil {
			r.add(Fail, "Password", err.Error(), "Run Set up again to save your password.")
		} else {
			r.add(OK, "Password", "saved in the system keychain", "")
		}
		if cfg.Paused {
			r.add(Warn, "Auto-login", "paused (you logged out or paused it)", "Choose Log in now to turn it back on.")
		}
	}

	if autostart.Enabled() {
		r.add(OK, "Start at login", "on", "")
	} else {
		r.add(Warn, "Start at login", "off", "Turn on Settings → Start at login, or autoportal won't run after a restart.")
	}

	switch {
	case in.InApp:
		r.add(OK, "Background app", "running", "")
	case running():
		r.add(OK, "Background app", "running", "")
	default:
		r.add(Warn, "Background app", "not running", "Open autoportal from your apps.")
	}
	if in.State != nil {
		r.add(Info, "Current state", in.State.Summary(), in.State.Detail)
	}
	if in.History != nil {
		r.add(Info, "History", in.History.Stats(r.When).Line(r.When), "")
	}

	if in.Updater != nil {
		switch {
		case latErr != nil:
			r.add(Info, "Updates", "could not check: "+latErr.Error(), "")
		case latest == in.Version:
			r.add(OK, "Updates", "up to date", "")
		default:
			r.add(Info, "Updates", "latest release is "+latest, "")
		}
	}

	r.LogTail = tail(in.LogPath, in.LogLines)
	return r
}

func (r *Report) add(s Status, name, detail, hint string) {
	r.Checks = append(r.Checks, Check{s, name, detail, hint})
}

// Problems counts the checks that failed.
func (r *Report) Problems() int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == Fail {
			n++
		}
	}
	return n
}

// WriteTo writes the report as plain text.
func (r *Report) WriteTo(w io.Writer) (int64, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "autoportal diagnostics, %s\n", r.When.Format("2006-01-02 15:04:05 MST"))
	b.WriteString("(This report contains no password.)\n\n")
	for _, h := range r.Header {
		b.WriteString(h + "\n")
	}
	b.WriteString("\n")
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "[%s] %-15s %s\n", c.Status, c.Name, c.Detail)
		if c.Hint != "" && c.Status != OK {
			fmt.Fprintf(&b, "       %-15s → %s\n", "", c.Hint)
		}
	}
	if len(r.LogTail) > 0 {
		b.WriteString("\n--- recent log ---\n")
		for _, l := range r.LogTail {
			b.WriteString(l + "\n")
		}
	}
	n, err := io.WriteString(w, b.String())
	return int64(n), err
}

// String returns the report text.
func (r *Report) String() string {
	var b strings.Builder
	_, _ = r.WriteTo(&b)
	return b.String()
}

func running() bool {
	dir, err := store.StateDir()
	if err != nil {
		return false
	}
	l, err := instance.Acquire(dir)
	if err == nil {
		l.Release()
	}
	return errors.Is(err, instance.ErrRunning)
}

// interfaces lists connected interfaces with their addresses.
func interfaces() []string {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		var v4 []string
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				v4 = append(v4, ipn.IP.String())
			}
		}
		if len(v4) > 0 {
			out = append(out, ifc.Name+" "+strings.Join(v4, " "))
		}
	}
	return out
}

// tail returns the last n lines of the file at path.
func tail(path string, n int) []string {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const chunk = 16 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > chunk {
		_, _ = f.Seek(-chunk, io.SeekEnd)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
