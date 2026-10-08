package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	"github.com/M-SaiCharan/autoportal/internal/portal"
	"github.com/M-SaiCharan/autoportal/internal/portal/sophos"
	"github.com/M-SaiCharan/autoportal/internal/portal/sophos/sophostest"
)

type fakeProber struct {
	mu   sync.Mutex
	conn netwatch.Connectivity
}

func (p *fakeProber) Probe(context.Context) netwatch.Connectivity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn
}

func (p *fakeProber) set(c netwatch.Connectivity) {
	p.mu.Lock()
	p.conn = c
	p.mu.Unlock()
}

type fakePortal struct {
	reachErr error
	loginErr error
	logins   int
	logouts  int
}

func (f *fakePortal) Reachable(context.Context) error { return f.reachErr }
func (f *fakePortal) Login(_ context.Context, u, _ string) (portal.Result, error) {
	f.logins++
	if f.loginErr != nil {
		return portal.Result{}, f.loginErr
	}
	return portal.Result{Message: "You are signed in as " + u}, nil
}
func (f *fakePortal) Logout(context.Context, string) (portal.Result, error) {
	f.logouts++
	return portal.Result{Message: "You've signed out"}, nil
}

type harness struct {
	a        *Agent
	p        *fakeProber
	f        *fakePortal
	notified []string
}

func newHarness() *harness {
	h := &harness{p: &fakeProber{}, f: &fakePortal{}}
	h.a = New(Options{
		Prober: h.p,
		Notify: func(title, _ string) { h.notified = append(h.notified, title) },
	})
	h.a.sess = &Session{Adapter: h.f, Username: "student", Password: "pw"}
	return h
}

func (h *harness) expect(t *testing.T, want Kind) {
	t.Helper()
	if got := h.a.State().Kind; got != want {
		t.Fatalf("state = %v (%q), want %v", got, h.a.State().Detail, want)
	}
}

var ctx = context.Background()

func TestLogsInWhenBlocked(t *testing.T) {
	h := newHarness()
	h.p.conn = netwatch.Blocked
	h.a.check(ctx)
	h.expect(t, SignedIn)
	if h.f.logins != 1 {
		t.Fatalf("logins = %d", h.f.logins)
	}
	if h.a.State().User != "student" || h.a.State().Since.IsZero() {
		t.Fatalf("state = %+v", h.a.State())
	}
}

func TestNoLoginWhenAlreadyOnline(t *testing.T) {
	h := newHarness()
	h.a.forceLogin = false
	h.p.conn = netwatch.Online
	h.a.check(ctx)
	h.expect(t, SignedIn)
	if h.f.logins != 0 {
		t.Fatalf("logged in needlessly (%d)", h.f.logins)
	}
}

func TestTriggerForcesOneLoginEvenIfOnline(t *testing.T) {
	// Covers campuses that whitelist the connectivity-check hosts.
	h := newHarness()
	h.p.conn = netwatch.Online
	h.a.check(ctx) // start-up: forced
	h.a.check(ctx) // steady state: not forced
	if h.f.logins != 1 {
		t.Fatalf("logins = %d, want 1", h.f.logins)
	}
	h.a.trigger()
	h.a.check(ctx)
	if h.f.logins != 2 {
		t.Fatalf("logins after trigger = %d, want 2", h.f.logins)
	}
}

func TestOffCampusAndNoNetwork(t *testing.T) {
	h := newHarness()
	h.f.reachErr = errors.New("timeout")
	h.p.conn = netwatch.Online
	h.a.check(ctx)
	h.expect(t, OffCampus)
	h.p.conn = netwatch.Offline
	h.a.check(ctx)
	h.expect(t, NoNetwork)
	if h.f.logins != 0 {
		t.Fatal("tried to log in off campus")
	}
	// Arriving on campus later still logs in (forceLogin survived).
	h.f.reachErr = nil
	h.p.conn = netwatch.Online
	h.a.check(ctx)
	h.expect(t, SignedIn)
	if h.f.logins != 1 {
		t.Fatalf("logins = %d, want 1", h.f.logins)
	}
}

func TestRejectedStopsRetrying(t *testing.T) {
	h := newHarness()
	h.p.conn = netwatch.Blocked
	h.f.loginErr = &portal.RejectedError{Message: "Invalid user name/password"}
	for i := 0; i < 5; i++ {
		h.a.check(ctx)
	}
	h.expect(t, Rejected)
	if h.f.logins != 1 {
		t.Fatalf("logins = %d; must not hammer the portal with a bad password", h.f.logins)
	}
	h.a.trigger() // a wake must not retry a known-bad password either
	h.a.check(ctx)
	if h.f.logins != 1 {
		t.Fatalf("retried after wake: logins = %d", h.f.logins)
	}
	if len(h.notified) != 1 {
		t.Fatalf("notifications = %v, want exactly one", h.notified)
	}
}

func TestChallengeHalts(t *testing.T) {
	h := newHarness()
	h.p.conn = netwatch.Blocked
	h.f.loginErr = portal.ErrChallenge
	h.a.check(ctx)
	h.a.check(ctx)
	h.expect(t, Challenge)
	if h.f.logins != 1 {
		t.Fatalf("logins = %d", h.f.logins)
	}
}

func TestCertChangedHaltsBeforeSendingPassword(t *testing.T) {
	h := newHarness()
	h.p.conn = netwatch.Blocked
	h.f.reachErr = &portal.CertMismatchError{Want: "AA", Got: "BB"}
	h.a.check(ctx)
	h.expect(t, CertChanged)
	if h.a.State().NewFingerprint != "BB" {
		t.Fatalf("state = %+v", h.a.State())
	}
	if h.f.logins != 0 {
		t.Fatal("password sent despite certificate change")
	}
}

func TestTransientErrorRetries(t *testing.T) {
	h := newHarness()
	h.p.conn = netwatch.Blocked
	h.f.loginErr = errors.New("connection reset")
	h.a.check(ctx)
	h.expect(t, PortalError)
	h.f.loginErr = nil
	h.a.check(ctx)
	h.expect(t, SignedIn)
}

func TestNeedsSetup(t *testing.T) {
	h := newHarness()
	h.a.sess = nil
	h.a.check(ctx)
	h.expect(t, NeedsSetup)
}

func TestSinceKeptWhileSignedIn(t *testing.T) {
	h := newHarness()
	now := time.Date(2026, 10, 6, 10, 42, 0, 0, time.UTC)
	h.a.opt.Now = func() time.Time { return now }
	h.p.conn = netwatch.Blocked
	h.a.check(ctx)
	first := h.a.State().Since
	now = now.Add(time.Hour)
	h.a.check(ctx) // logs in again (still blocked) but stays the same session
	if got := h.a.State().Since; !got.Equal(first) {
		t.Fatalf("Since moved from %v to %v", first, got)
	}
}

func TestNextDelay(t *testing.T) {
	h := newHarness()
	h.a.set(State{Kind: NoNetwork})
	if d := h.a.nextDelay(); d != h.a.opt.Interval {
		t.Fatalf("idle delay = %v", d)
	}
	h.a.trigger()
	if d := h.a.nextDelay(); d != h.a.opt.BurstInterval {
		t.Fatalf("burst delay = %v", d)
	}
	h.a.set(State{Kind: SignedIn})
	if d := h.a.nextDelay(); d != h.a.opt.Interval {
		t.Fatalf("signed-in delay during burst = %v", d)
	}
}

// TestRunEndToEnd drives the real loop against the fake Sophos server:
// start-up login, user logout (pause), wake trigger while paused, resume.
func TestRunEndToEnd(t *testing.T) {
	srv := sophostest.NewTLS(t, map[string]string{"student": "pw"})
	p := &fakeProber{conn: netwatch.Blocked}
	events := make(chan netwatch.Event)
	states := make(chan State, 64)
	a := New(Options{Prober: p, Events: events, Interval: time.Hour})
	a.Subscribe(func(s State) { states <- s })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor := func(k Kind) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case s := <-states:
				if s.Kind == k {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %v (now %v)", k, a.State().Kind)
			}
		}
	}

	waitFor(NeedsSetup)
	sess := &Session{Adapter: sophos.New(srv.Endpoint(t)), Username: "student", Password: "pw"}
	if err := a.Configure(ctx, sess); err != nil {
		t.Fatal(err)
	}
	waitFor(SignedIn)
	if !srv.LoggedIn("student") {
		t.Fatal("not logged in on the server")
	}

	if err := a.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(Paused)
	if srv.LoggedIn("student") {
		t.Fatal("still logged in on the server")
	}

	before := srv.Logins()
	events <- netwatch.Event{Reason: "wake"}
	if err := a.SetPaused(ctx, true); err != nil { // round-trip to make sure the event was handled
		t.Fatal(err)
	}
	if srv.Logins() != before {
		t.Fatal("logged in while paused")
	}

	if err := a.LoginNow(ctx); err != nil {
		t.Fatal(err)
	}
	if !srv.LoggedIn("student") {
		t.Fatal("LoginNow did not log in")
	}
}

func TestOnLoginCountsOnlyRestoringLogins(t *testing.T) {
	var at []time.Time
	h := newHarness()
	h.a.opt.OnLogin = func(t time.Time) { at = append(at, t) }

	h.p.conn = netwatch.Online
	h.a.check(ctx) // precautionary start-up login while online: not counted
	if h.f.logins != 1 || len(at) != 0 {
		t.Fatalf("logins=%d recorded=%d", h.f.logins, len(at))
	}
	h.p.conn = netwatch.Blocked
	h.a.check(ctx) // session dropped, login restores it: counted
	if h.f.logins != 2 || len(at) != 1 {
		t.Fatalf("logins=%d recorded=%d", h.f.logins, len(at))
	}
}

func TestNotifyOnLoginToggle(t *testing.T) {
	h := newHarness()
	h.p.conn = netwatch.Blocked
	h.a.check(ctx)
	if len(h.notified) != 0 {
		t.Fatalf("notified by default: %v", h.notified)
	}
	h.a.SetNotifyOnLogin(true)
	h.a.set(State{Kind: OffCampus})
	h.a.check(ctx)
	if len(h.notified) != 1 || h.notified[0] != "Signed in" {
		t.Fatalf("notified = %v", h.notified)
	}
}
