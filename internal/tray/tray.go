// Package tray is the menu-bar (macOS) / system-tray (Windows, Linux) UI.
package tray

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"fyne.io/systray"

	"github.com/M-SaiCharan/autoportal/internal/agent"
	"github.com/M-SaiCharan/autoportal/internal/app"
	"github.com/M-SaiCharan/autoportal/internal/autostart"
	"github.com/M-SaiCharan/autoportal/internal/captive"
	"github.com/M-SaiCharan/autoportal/internal/doctor"
	"github.com/M-SaiCharan/autoportal/internal/update"
)

// Options configures Run.
type Options struct {
	Version string
	// Updated receives the new version after an update was installed;
	// the tray then quits with restart set.
	Updated <-chan string
	// OnExit runs when the tray quits. On macOS the process ends right
	// after it returns, so all clean-up belongs here.
	OnExit func(restart bool)
}

// Run shows the tray icon and blocks until the user quits. It must be
// called from the main goroutine.
func Run(ctx context.Context, a *app.App, opt Options) {
	var restart atomic.Bool
	systray.Run(func() { onReady(ctx, a, opt, &restart) }, func() {
		if opt.OnExit != nil {
			opt.OnExit(restart.Load())
		}
	})
}

type menu struct {
	status, history, login, logout, pause, setup, diag, log, quit *systray.MenuItem
	autostart, autoUpdate, notifyLogin, macPopup, checkUpdate     *systray.MenuItem
}

func onReady(ctx context.Context, a *app.App, opt Options, restart *atomic.Bool) {
	systray.SetIcon(iconBytes(agent.Checking))
	systray.SetTooltip("autoportal")

	m := menu{}
	m.status = systray.AddMenuItem("Checking…", "")
	m.status.Disable()
	m.history = systray.AddMenuItem("", "")
	m.history.Disable()
	m.history.Hide()
	systray.AddSeparator()
	m.login = systray.AddMenuItem("Log in now", "Sign in to the portal right away")
	m.logout = systray.AddMenuItem("Log out", "Sign out and pause automatic login")
	m.pause = systray.AddMenuItemCheckbox("Pause auto-login", "Stop logging in automatically", false)
	systray.AddSeparator()
	m.setup = systray.AddMenuItem("Change credentials…", "Change the username, password or portal")
	settings := systray.AddMenuItem("Settings", "")
	m.autostart = settings.AddSubMenuItemCheckbox("Start at login", "Start autoportal when you log in", autostart.Enabled())
	m.autoUpdate = settings.AddSubMenuItemCheckbox("Install updates automatically",
		"Download and install new versions of autoportal in the background", a.AutoUpdate())
	m.notifyLogin = settings.AddSubMenuItemCheckbox("Notify me after every login",
		"Show a notification each time autoportal signs you in", a.NotifyOnLogin())
	if captive.Supported() {
		m.macPopup = settings.AddSubMenuItemCheckbox("Hide the macOS Wi-Fi login window",
			"Stop macOS from opening its own login window on campus Wi-Fi (asks for your Mac password)",
			captive.PopupDisabled())
	}
	m.checkUpdate = settings.AddSubMenuItem("Check for updates now", "")
	m.diag = systray.AddMenuItem("Copy diagnostics", "Check everything and copy a report you can send to whoever is helping you")
	m.log = systray.AddMenuItem("Open log file", "")
	systray.AddSeparator()
	about := systray.AddMenuItem("autoportal "+opt.Version, "")
	about.Disable()
	m.quit = systray.AddMenuItem("Quit", "Quit autoportal (you stay signed in)")

	// The agent calls back on its own goroutine; keep only the latest state.
	updates := make(chan agent.State, 1)
	push := func(st agent.State) {
		select {
		case <-updates:
		default:
		}
		updates <- st
	}
	a.Agent.Subscribe(push)
	push(a.Agent.State())

	var busy atomic.Bool // one dialog/action at a time
	idle := make(chan struct{}, 1)
	run := func(fn func()) {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer func() {
				busy.Store(false)
				select {
				case idle <- struct{}{}:
				default:
				}
			}()
			fn()
		}()
	}

	if a.Config() == nil {
		run(func() { runSetup(ctx, a) })
	}

	setChecked := func(item *systray.MenuItem, on bool) {
		if on {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	refresh := time.NewTicker(30 * time.Second) // keeps "5 min ago" current
	var pendingUpdate string

	go func() {
		defer refresh.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case st := <-updates:
				m.apply(st)
				m.showHistory(a, st)
			case <-refresh.C:
				m.showHistory(a, a.Agent.State())
			case <-idle:
				if pendingUpdate != "" && !busy.Load() {
					restart.Store(true)
					systray.Quit()
					return
				}
			case v := <-opt.Updated:
				a.Log.Info("restarting into new version", "version", v)
				Notify("autoportal updated", "Now running "+v+".")
				if !busy.Load() {
					restart.Store(true)
					systray.Quit()
					return
				}
				pendingUpdate = v // a dialog is open; restart once it closes
			case <-m.login.ClickedCh:
				run(func() {
					if err := a.LoginNow(ctx); err != nil {
						Notify("Could not log in", err.Error())
					}
				})
			case <-m.logout.ClickedCh:
				run(func() {
					if err := a.Logout(ctx); err != nil {
						Notify("Could not log out", err.Error())
					}
				})
			case <-m.pause.ClickedCh:
				paused := !m.pause.Checked()
				run(func() { _ = a.SetPaused(ctx, paused) })
			case <-m.setup.ClickedCh:
				run(func() {
					runSetup(ctx, a)
					setChecked(m.autostart, autostart.Enabled())
					setChecked(m.autoUpdate, a.AutoUpdate())
					setChecked(m.notifyLogin, a.NotifyOnLogin())
				})
			case <-m.autostart.ClickedCh:
				run(func() {
					if m.autostart.Checked() {
						if err := autostart.Disable(); err != nil {
							showError("Could not disable start at login: " + err.Error())
						}
					} else {
						installAutostart(a)
					}
					setChecked(m.autostart, autostart.Enabled())
				})
			case <-m.autoUpdate.ClickedCh:
				on := !m.autoUpdate.Checked()
				if err := a.SetAutoUpdate(on); err != nil {
					showError(err.Error())
				}
				setChecked(m.autoUpdate, a.AutoUpdate())
			case <-m.notifyLogin.ClickedCh:
				on := !m.notifyLogin.Checked()
				if err := a.SetNotifyOnLogin(on); err != nil {
					showError(err.Error())
				}
				setChecked(m.notifyLogin, a.NotifyOnLogin())
			case <-macPopupClicks(m.macPopup):
				off := !m.macPopup.Checked()
				run(func() {
					err := captive.SetPopupDisabled(off)
					if err != nil && !errors.Is(err, captive.ErrCancelled) {
						showError("Could not change the macOS setting: " + err.Error())
					}
					setChecked(m.macPopup, captive.PopupDisabled())
				})
			case <-m.checkUpdate.ClickedCh:
				run(func() { checkForUpdates(ctx, a, opt.Version) })
			case <-m.diag.ClickedCh:
				run(func() { copyDiagnostics(ctx, a, opt.Version) })
			case <-m.log.ClickedCh:
				_ = openFile(a.LogPath)
			case <-m.quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// macPopupClicks returns the item's click channel, or nil (never ready)
// on systems without the item.
func macPopupClicks(item *systray.MenuItem) <-chan struct{} {
	if item == nil {
		return nil
	}
	return item.ClickedCh
}

func (m *menu) apply(st agent.State) {
	systray.SetIcon(iconBytes(st.Kind))
	summary := st.Summary()
	systray.SetTooltip("autoportal: " + summary)
	m.status.SetTitle(summary)

	if st.Kind == agent.NeedsSetup {
		m.setup.SetTitle("Set up…")
	} else {
		m.setup.SetTitle("Change credentials…")
	}
	if st.Kind == agent.Paused {
		m.pause.Check()
	} else {
		m.pause.Uncheck()
	}
	for _, item := range []*systray.MenuItem{m.login, m.logout, m.pause, m.autoUpdate, m.notifyLogin} {
		if st.Kind == agent.NeedsSetup {
			item.Disable()
		} else {
			item.Enable()
		}
	}
}

func (m *menu) showHistory(a *app.App, st agent.State) {
	if st.Kind == agent.NeedsSetup {
		m.history.Hide()
		return
	}
	now := time.Now()
	m.history.SetTitle(a.History.Stats(now).Line(now))
	m.history.Show()
}

func checkForUpdates(ctx context.Context, a *app.App, version string) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	tag, err := a.CheckForUpdates(ctx)
	switch {
	case errors.Is(err, update.ErrDevBuild):
		showInfo("This is a development build (" + version + "), so it doesn't update itself.\n\n" +
			"Install a release from github.com/" + update.Repo + " to get automatic updates.")
	case err != nil:
		showError("Could not check for updates: " + err.Error())
	case tag == "":
		showInfo("You have the latest version (" + version + ").")
	}
	// A successful update arrives on Options.Updated and restarts the app.
}

func copyDiagnostics(ctx context.Context, a *app.App, version string) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	st := a.Agent.State()
	rep := doctor.Run(ctx, doctor.Inputs{
		Version: version, Exe: a.Exe, LogPath: a.LogPath, History: a.History,
		State: &st, InApp: true, Updater: a.Updater,
	})
	text := rep.String()
	path := filepath.Join(filepath.Dir(a.LogPath), "diagnostics.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		showError("Could not write the diagnostics report: " + err.Error())
		return
	}
	copied := copyText(text) == nil
	_ = openFile(path)
	if copied {
		Notify("Diagnostics copied", "Paste it into a message to whoever is helping you. It contains no password.")
	}
}
