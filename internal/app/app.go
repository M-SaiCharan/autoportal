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
	"github.com/M-SaiCharan/autoportal/internal/instance"
	"github.com/M-SaiCharan/autoportal/internal/logx"
	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	"github.com/M-SaiCharan/autoportal/internal/store"

	_ "github.com/M-SaiCharan/autoportal/internal/portal/sophos" // register driver
)

// Options configures Start.
type Options struct {
	Notify func(title, text string) // desktop notifications; may be nil
	Stderr bool                     // also log to stderr (headless mode)
	Debug  bool
}

// App is a running autoportal instance.
type App struct {
	Log     *slog.Logger
	LogPath string
	Agent   *agent.Agent

	lock    *instance.Lock
	logFile *logx.RotatingFile

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

	a := &App{Log: log, LogPath: logPath, lock: lock, logFile: lf}
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
	})
	log.Info("started", "configured", a.cfg != nil, "paused", paused)
	go a.Agent.Run(ctx)
	return a, nil
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

// Close releases the lock and closes the log.
func (a *App) Close() error {
	a.Log.Info("stopped")
	return errors.Join(a.lock.Release(), a.logFile.Close())
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
