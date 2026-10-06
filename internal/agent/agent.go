// Package agent is the background brain: it decides when to check the
// network, when to log in, and when to stop and ask the user.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	"github.com/M-SaiCharan/autoportal/internal/portal"
)

// Kind is the coarse state shown as the tray icon.
type Kind int

const (
	NeedsSetup  Kind = iota // no credentials yet
	Checking                // first check not finished
	SignedIn                // portal reachable and internet works
	SigningIn               // login request in flight
	OffCampus               // on some other network
	NoNetwork               // no network at all
	PortalError             // portal reachable but the request failed; will retry
	Rejected                // portal refused the credentials; waits for the user
	Challenge               // portal wants an extra step; waits for the user
	CertChanged             // portal certificate differs from the pinned one; waits for the user
	Paused                  // user logged out or paused auto-login
)

var kindNames = [...]string{"needs-setup", "checking", "signed-in", "signing-in", "off-campus",
	"no-network", "portal-error", "rejected", "challenge", "cert-changed", "paused"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// NeedsUser reports whether the agent has stopped until the user acts.
func (k Kind) NeedsUser() bool {
	return k == NeedsSetup || k == Rejected || k == Challenge || k == CertChanged
}

// State is a snapshot for the UI.
type State struct {
	Kind   Kind
	Detail string    // extra human-readable detail, may be empty
	User   string    // configured username
	Since  time.Time // when the current SignedIn period started
	// NewFingerprint is the certificate the portal now presents (CertChanged).
	NewFingerprint string
}

// Summary is a one-line description for menus and the CLI.
func (s State) Summary() string {
	switch s.Kind {
	case NeedsSetup:
		return "Not set up yet"
	case Checking:
		return "Checking network…"
	case SignedIn:
		if s.Since.IsZero() {
			return "Signed in as " + s.User
		}
		return fmt.Sprintf("Signed in as %s · since %s", s.User, s.Since.Local().Format("15:04"))
	case SigningIn:
		return "Signing in…"
	case OffCampus:
		return "Not on the campus network"
	case NoNetwork:
		return "No network connection"
	case PortalError:
		return "Portal not responding, retrying…"
	case Rejected:
		return "Login rejected, check your password"
	case Challenge:
		return "Portal asks for an extra step, log in manually"
	case CertChanged:
		return "Portal certificate changed, re-run setup"
	case Paused:
		return "Paused (auto-login off)"
	}
	return s.Kind.String()
}

// Prober reports internet connectivity.
type Prober interface {
	Probe(ctx context.Context) netwatch.Connectivity
}

// Session is what the agent needs to log in.
type Session struct {
	Adapter  portal.Adapter
	Username string
	Password string
}

// Options configures an Agent. Zero durations get sensible defaults.
type Options struct {
	Prober Prober
	Events <-chan netwatch.Event // wake / network-change triggers; may be nil
	Logger *slog.Logger
	// Notify shows a desktop notification; may be nil.
	Notify func(title, text string)
	// NotifyOnLogin also notifies after every successful automatic login.
	NotifyOnLogin bool
	// StartPaused starts with auto-login off (the user logged out earlier).
	StartPaused bool
	// Session is the initial session; nil means not set up.
	Session *Session

	Interval      time.Duration // steady-state check period (default 60s)
	Burst         time.Duration // how long to check rapidly after a trigger (default 60s)
	BurstInterval time.Duration // rapid check period (default 3s)
	Now           func() time.Time
}

// Agent runs the check/login loop. All mutable fields are owned by the Run
// goroutine; other goroutines talk to it through commands.
type Agent struct {
	opt  Options
	log  *slog.Logger
	cmds chan command

	// Owned by the Run goroutine.
	sess       *Session
	paused     bool
	halted     bool      // stopped until the user acts (rejected, challenge, cert)
	forceLogin bool      // send a login even if the probe says online
	burstUntil time.Time // check rapidly until then

	mu        sync.Mutex
	state     State
	listeners []func(State)
}

type command struct {
	fn   func(ctx context.Context) error
	done chan error
}

// New creates an agent. Call Run to start it.
func New(opt Options) *Agent {
	if opt.Interval == 0 {
		opt.Interval = 60 * time.Second
	}
	if opt.Burst == 0 {
		opt.Burst = 60 * time.Second
	}
	if opt.BurstInterval == 0 {
		opt.BurstInterval = 3 * time.Second
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.DiscardHandler)
	}
	return &Agent{
		opt:        opt,
		log:        opt.Logger,
		cmds:       make(chan command),
		paused:     opt.StartPaused,
		sess:       opt.Session,
		forceLogin: true, // first check after start-up always logs in
		state:      State{Kind: Checking},
	}
}

// State returns the current state.
func (a *Agent) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// Subscribe registers fn to be called (on the agent goroutine) whenever
// the state changes. Call it before Run.
func (a *Agent) Subscribe(fn func(State)) {
	a.mu.Lock()
	a.listeners = append(a.listeners, fn)
	a.mu.Unlock()
}

// Run checks the network until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	timer := time.NewTimer(0) // first check right away
	defer timer.Stop()
	events := a.opt.Events
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			a.log.Info("trigger", "reason", ev.Reason)
			a.trigger()
			a.check(ctx)
		case c := <-a.cmds:
			c.done <- c.fn(ctx)
		case <-timer.C:
			a.check(ctx)
		}
		timer.Reset(a.nextDelay())
	}
}

func (a *Agent) nextDelay() time.Duration {
	k := a.State().Kind
	if a.opt.Now().Before(a.burstUntil) && !a.paused && !a.halted && k != SignedIn {
		return a.opt.BurstInterval
	}
	return a.opt.Interval
}

// trigger is called on wake, network change and user actions: log in on
// the next opportunity and check rapidly for a while, because the network
// usually needs a few seconds to come up after waking.
func (a *Agent) trigger() {
	a.forceLogin = true
	a.burstUntil = a.opt.Now().Add(a.opt.Burst)
}

// do runs fn on the agent goroutine and waits for it.
func (a *Agent) do(ctx context.Context, fn func(ctx context.Context) error) error {
	c := command{fn: fn, done: make(chan error, 1)}
	select {
	case a.cmds <- c:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-c.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Configure installs new credentials (nil = not set up) and checks at once.
func (a *Agent) Configure(ctx context.Context, s *Session) error {
	return a.do(ctx, func(ctx context.Context) error {
		a.sess = s
		a.halted = false
		a.trigger()
		a.check(ctx)
		return nil
	})
}

// LoginNow clears pause and any stop-and-ask state, then logs in.
func (a *Agent) LoginNow(ctx context.Context) error {
	return a.do(ctx, func(ctx context.Context) error {
		a.paused = false
		a.halted = false
		a.trigger()
		a.check(ctx)
		if st := a.State(); st.Kind != SignedIn {
			return errors.New(st.Summary())
		}
		return nil
	})
}

// SetPaused turns automatic login off or back on.
func (a *Agent) SetPaused(ctx context.Context, paused bool) error {
	return a.do(ctx, func(ctx context.Context) error {
		a.paused = paused
		if !paused {
			a.trigger()
		}
		a.check(ctx)
		return nil
	})
}

// Logout signs out and pauses auto-login so it doesn't immediately sign
// back in.
func (a *Agent) Logout(ctx context.Context) error {
	return a.do(ctx, func(ctx context.Context) error {
		a.paused = true
		if a.sess == nil {
			a.check(ctx)
			return errors.New("not set up")
		}
		res, err := a.sess.Adapter.Logout(ctx, a.sess.Username)
		if err != nil {
			a.log.Warn("logout failed", "err", err)
			a.set(State{Kind: Paused, Detail: "logout failed: " + err.Error()})
			return err
		}
		a.log.Info("logged out", "message", res.Message)
		a.set(State{Kind: Paused, Detail: res.Message})
		return nil
	})
}

// check is one round of: probe → reachability → maybe log in.
func (a *Agent) check(ctx context.Context) {
	if a.paused {
		a.set(State{Kind: Paused})
		return
	}
	if a.sess == nil {
		a.set(State{Kind: NeedsSetup})
		return
	}
	if a.halted {
		return
	}
	s := a.sess

	conn := a.opt.Prober.Probe(ctx)
	rerr := s.Adapter.Reachable(ctx)
	switch {
	case a.certChanged(rerr):
		return
	case rerr != nil:
		// Keep forceLogin: if this is right after a wake, we still want to
		// log in as soon as the campus network shows up.
		kind := OffCampus
		if conn == netwatch.Offline {
			kind = NoNetwork
		}
		if a.State().Kind != kind {
			a.log.Info("portal not reachable", "connectivity", conn.String(), "err", rerr)
		}
		a.set(State{Kind: kind})
	case conn == netwatch.Online && !a.forceLogin:
		a.setSignedIn("")
	default:
		a.login(ctx, conn)
	}
}

func (a *Agent) login(ctx context.Context, conn netwatch.Connectivity) {
	s := a.sess
	prev := a.State().Kind
	if prev != SignedIn {
		a.set(State{Kind: SigningIn})
	}
	res, err := s.Adapter.Login(ctx, s.Username, s.Password)
	switch {
	case err == nil:
		a.forceLogin = false
		a.log.Info("login ok", "connectivity_before", conn.String(), "message", res.Message)
		a.setSignedIn(res.Message)
		if prev != SignedIn && a.opt.NotifyOnLogin {
			a.notify("Signed in", res.Message)
		}
	case portal.IsRejected(err):
		a.forceLogin = false
		a.log.Warn("login rejected", "err", err)
		a.halt(State{Kind: Rejected, Detail: err.Error()})
		a.notify("Login rejected", "The portal refused your username or password. Open the autoportal menu → Change credentials.")
	case errors.Is(err, portal.ErrChallenge):
		a.forceLogin = false
		a.log.Warn("login challenge", "err", err)
		a.halt(State{Kind: Challenge, Detail: err.Error()})
		a.notify("Manual login needed", "The portal asked for an extra verification step. Please log in from the browser.")
	case a.certChanged(err):
	default:
		a.log.Warn("login failed", "err", err)
		a.set(State{Kind: PortalError, Detail: err.Error()})
	}
}

// certChanged handles a pin mismatch; it reports whether err was one.
func (a *Agent) certChanged(err error) bool {
	var mm *portal.CertMismatchError
	if !errors.As(err, &mm) {
		return false
	}
	a.log.Warn("portal certificate changed", "want", mm.Want, "got", mm.Got)
	a.halt(State{Kind: CertChanged, Detail: mm.Error(), NewFingerprint: mm.Got})
	a.notify("Portal certificate changed",
		"autoportal did not send your password. If your college renewed its certificate, open the menu → Set up again.")
	return true
}

func (a *Agent) setSignedIn(detail string) {
	st := State{Kind: SignedIn, Detail: detail, Since: a.opt.Now()}
	if cur := a.State(); cur.Kind == SignedIn {
		st.Since = cur.Since
		if detail == "" {
			st.Detail = cur.Detail
		}
	}
	a.set(st)
}

func (a *Agent) halt(st State) {
	a.halted = true
	a.set(st)
}

func (a *Agent) notify(title, text string) {
	if a.opt.Notify != nil {
		a.opt.Notify(title, text)
	}
}

func (a *Agent) set(st State) {
	if a.sess != nil {
		st.User = a.sess.Username
	}
	a.mu.Lock()
	changed := st != a.state
	a.state = st
	ls := append([]func(State){}, a.listeners...)
	a.mu.Unlock()
	if !changed {
		return
	}
	a.log.Debug("state", "kind", st.Kind.String(), "detail", st.Detail)
	for _, fn := range ls {
		fn(st)
	}
}
