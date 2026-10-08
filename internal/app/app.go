// Package app wires the agent to the config, keychain, log file and
// network watcher. Both the tray UI and the headless mode use it.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/M-SaiCharan/autoportal/internal/agent"
	"github.com/M-SaiCharan/autoportal/internal/autostart"
	"github.com/M-SaiCharan/autoportal/internal/history"
	"github.com/M-SaiCharan/autoportal/internal/instance"
	"github.com/M-SaiCharan/autoportal/internal/logx"
	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	"github.com/M-SaiCharan/autoportal/internal/store"
	"github.com/M-SaiCharan/autoportal/internal/update"

	_ "github.com/M-SaiCharan/autoportal/internal/portal/sophos" // register driver
)

// Options configures Start.
type Options struct {
	Notify func(title, text string) // desktop notifications; may be nil
	Stderr bool                     // also log to stderr (headless mode)
	Debug  bool
	// Version is the running release, e.g. "v0.2.0"; "dev" builds never update.
	Version string
	// OnUpdated is called after a new release has been installed; the
	// caller should then quit and Restart. Nil disables automatic updates.
	OnUpdated func(version string)
}

// App is a running autoportal instance.
type App struct {
	Log     *slog.Logger
	LogPath string
	Exe     string // this program, resolved at start-up (before any update moves it)
	Agent   *agent.Agent
	History *history.History
	Updater *update.Updater

	opt       Options
	lock      *instance.Lock
	logFile   *logx.RotatingFile
	closeOnce sync.Once
	updating  sync.Mutex

	mu  sync.Mutex
	cfg *store.Config // nil until set up
}

// ErrRunning is returned by Start when another instance is active.
var ErrRunning = instance.ErrRunning

// Start acquires the single-instance lock, opens the log, loads the
// config and starts the agent in the background.
func Start(ctx context.Context, opt Options) (*App, error) {
	dir, err := store.StateDir()
	if err != nil {
		return nil, err
	}
	lock, err := instance.Acquire(dir)
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "autoportal.log")
	lf, err := logx.Open(logPath, 1<<20)
	if err != nil {
		lock.Release()
		return nil, err
	}
	var w io.Writer = lf
	if opt.Stderr {
		w = io.MultiWriter(lf, os.Stderr)
	}
	log := logx.New(w, opt.Debug)

	exe, err := autostart.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	update.Cleanup(exe)
	a := &App{
		Log: log, LogPath: logPath, Exe: exe, opt: opt,
		History: history.Open(filepath.Join(dir, "history.json")),
		Updater: &update.Updater{Current: opt.Version, Exe: exe},
	}
	a.lock, a.logFile = lock, lf
	cfg, err := store.Load()
	switch {
	case errors.Is(err, store.ErrNotConfigured):
		log.Info("not configured yet")
	case err != nil:
		log.Error("cannot load config", "err", err)
	default:
		a.cfg = cfg
	}

	var sess *agent.Session
	if a.cfg != nil {
		sess, err = sessionFor(ctx, a.cfg)
		if err != nil {
			log.Error("cannot prepare session", "err", err)
		}
	}
	notifyOnLogin := a.cfg != nil && a.cfg.NotifyOnLogin
	paused := a.cfg != nil && a.cfg.Paused
	notify := func(title, text string) {
		log.Info("notify", "title", title, "text", text)
		if opt.Notify != nil {
			opt.Notify(title, text)
		}
	}

	a.Agent = agent.New(agent.Options{
		Prober:        netwatch.NewProber(),
		Events:        netwatch.NewWatcher().Watch(ctx),
		Logger:        log,
		Notify:        notify,
		NotifyOnLogin: notifyOnLogin,
		StartPaused:   paused,
		Session:       sess,
		OnLogin: func(at time.Time) {
			if err := a.History.Record(at); err != nil {
				log.Warn("cannot save login history", "err", err)
			}
		},
	})
	log.Info("started", "version", opt.Version, "configured", a.cfg != nil, "paused", paused)
	go a.Agent.Run(ctx)
	if opt.OnUpdated != nil {
		go a.updateLoop(ctx)
	}
	return a, nil
}

// Update timing: the first check waits until the network has settled after
// start-up, then one check a day. Checks are cheap (one HEAD request).
const (
	firstUpdateCheck = 2 * time.Minute
	updateEvery      = 24 * time.Hour
	updateRetry      = time.Hour
)

func (a *App) updateLoop(ctx context.Context) {
	if err := a.Updater.Supported(); err != nil {
		a.Log.Info("automatic updates off", "reason", err)
		return
	}
	var last time.Time // wall clock, so a laptop asleep for days checks on wake
	wait := firstUpdateCheck
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = updateRetry
		if !a.AutoUpdate() || time.Now().Round(0).Sub(last) < updateEvery {
			continue
		}
		tag, err := a.update(ctx)
		if err != nil {
			a.Log.Info("update check failed", "err", err)
			continue
		}
		last = time.Now().Round(0)
		if tag != "" {
			return // OnUpdated restarts the app
		}
	}
}

// CheckForUpdates checks now and installs a newer release if there is one.
// It returns the installed version, or "" when already up to date.
func (a *App) CheckForUpdates(ctx context.Context) (string, error) {
	if err := a.Updater.Supported(); err != nil {
		return "", err
	}
	return a.update(ctx)
}

func (a *App) update(ctx context.Context) (string, error) {
	if !a.updating.TryLock() {
		return "", errors.New("an update is already in progress")
	}
	defer a.updating.Unlock()
	tag, err := a.Updater.Check(ctx)
	if err != nil || tag == "" {
		return "", err
	}
	a.Log.Info("installing update", "from", a.opt.Version, "to", tag)
	if err := a.Updater.Apply(ctx, tag); err != nil {
		a.Log.Warn("update failed", "to", tag, "err", err)
		return "", err
	}
	a.Log.Info("update installed", "version", tag)
	if a.opt.OnUpdated != nil {
		a.opt.OnUpdated(tag)
	}
	return tag, nil
}

// AutoUpdate reports whether new releases are installed automatically.
func (a *App) AutoUpdate() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg == nil || !a.cfg.NoAutoUpdate
}

// SetAutoUpdate turns automatic updates on or off.
func (a *App) SetAutoUpdate(on bool) error {
	return a.saveSetting(func(c *store.Config) { c.NoAutoUpdate = !on })
}

// NotifyOnLogin reports whether every login is announced.
func (a *App) NotifyOnLogin() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg != nil && a.cfg.NotifyOnLogin
}

// SetNotifyOnLogin turns login notifications on or off.
func (a *App) SetNotifyOnLogin(on bool) error {
	if err := a.saveSetting(func(c *store.Config) { c.NotifyOnLogin = on }); err != nil {
		return err
	}
	a.Agent.SetNotifyOnLogin(on)
	return nil
}

func (a *App) saveSetting(change func(*store.Config)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return errors.New("set up autoportal first")
	}
	c := *a.cfg
	change(&c)
	if err := store.Save(&c); err != nil {
		return err
	}
	a.cfg = &c
	return nil
}

// sessionFor reads the password from the keychain. Right after login the
// keychain service can lag a moment behind autostart, so retry briefly.
func sessionFor(ctx context.Context, c *store.Config) (*agent.Session, error) {
	ad, err := c.Adapter()
	if err != nil {
		return nil, err
	}
	var pw string
	for i := 0; ; i++ {
		pw, err = store.GetPassword(c)
		if err == nil || i == 4 {
			break
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &agent.Session{Adapter: ad, Username: c.Username, Password: pw}, nil
}

// Config returns a copy of the current config, or nil if not set up.
func (a *App) Config() *store.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return nil
	}
	c := *a.cfg
	return &c
}

// Reload re-reads config and password (after setup) and applies them.
func (a *App) Reload(ctx context.Context) error {
	cfg, err := store.Load()
	if err != nil {
		return err
	}
	sess, err := sessionFor(ctx, cfg)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	a.Log.Info("config reloaded", "user", cfg.Username, "portal", cfg.Portal.URL)
	a.Agent.SetNotifyOnLogin(cfg.NotifyOnLogin)
	if err := a.Agent.SetPaused(ctx, cfg.Paused); err != nil {
		return err
	}
	return a.Agent.Configure(ctx, sess)
}

// LoginNow resumes and logs in immediately.
func (a *App) LoginNow(ctx context.Context) error {
	a.persistPaused(false)
	return a.Agent.LoginNow(ctx)
}

// Logout signs out and pauses auto-login (remembered across restarts).
func (a *App) Logout(ctx context.Context) error {
	a.persistPaused(true)
	return a.Agent.Logout(ctx)
}

// SetPaused turns auto-login off or on (remembered across restarts).
func (a *App) SetPaused(ctx context.Context, paused bool) error {
	a.persistPaused(paused)
	return a.Agent.SetPaused(ctx, paused)
}

func (a *App) persistPaused(paused bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil || a.cfg.Paused == paused {
		return
	}
	a.cfg.Paused = paused
	if err := store.Save(a.cfg); err != nil {
		a.Log.Warn("cannot save paused state", "err", err)
	}
}

// Close releases the lock and closes the log. It is safe to call twice.
func (a *App) Close() error {
	var err error
	a.closeOnce.Do(func() {
		a.Log.Info("stopped")
		err = errors.Join(a.lock.Release(), a.logFile.Close())
	})
	return err
}

// Describe returns a multi-line status report for the CLI.
func Describe(st agent.State, cfg *store.Config) string {
	s := st.Summary()
	if st.Detail != "" && st.Kind != agent.SignedIn {
		s += "\n  " + st.Detail
	}
	if cfg != nil {
		s += fmt.Sprintf("\n  portal: %s (%s)", cfg.Portal.URL, cfg.Portal.Title)
	}
	return s
}
