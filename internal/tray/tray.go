// Package tray is the menu-bar (macOS) / system-tray (Windows, Linux) UI.
package tray

import (
	"context"
	"sync/atomic"

	"fyne.io/systray"

	"github.com/M-SaiCharan/autoportal/internal/agent"
	"github.com/M-SaiCharan/autoportal/internal/app"
	"github.com/M-SaiCharan/autoportal/internal/autostart"
)

// Run shows the tray icon and blocks until the user quits. It must be
// called from the main goroutine. cancel stops the app's context.
func Run(ctx context.Context, cancel context.CancelFunc, a *app.App, version string) {
	systray.Run(func() { onReady(ctx, a, version) }, cancel)
}

type menu struct {
	status, login, logout, pause, setup, autostart, log, quit *systray.MenuItem
}

func onReady(ctx context.Context, a *app.App, version string) {
	systray.SetIcon(iconBytes(agent.Checking))
	systray.SetTooltip("autoportal")

	m := menu{}
	m.status = systray.AddMenuItem("Checking…", "")
	m.status.Disable()
	systray.AddSeparator()
	m.login = systray.AddMenuItem("Log in now", "Sign in to the portal right away")
	m.logout = systray.AddMenuItem("Log out", "Sign out and pause automatic login")
	m.pause = systray.AddMenuItemCheckbox("Pause auto-login", "Stop logging in automatically", false)
	systray.AddSeparator()
	m.setup = systray.AddMenuItem("Change credentials…", "Change the username, password or portal")
	m.autostart = systray.AddMenuItemCheckbox("Start at login", "Start autoportal when you log in", autostart.Enabled())
	m.log = systray.AddMenuItem("Open log file", "")
	systray.AddSeparator()
	about := systray.AddMenuItem("autoportal "+version, "")
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
	run := func(fn func()) {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer busy.Store(false)
			fn()
		}()
	}

	if a.Config() == nil {
		run(func() { runSetup(ctx, a) })
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case st := <-updates:
				m.apply(st)
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
				run(func() { runSetup(ctx, a) })
			case <-m.autostart.ClickedCh:
				run(func() {
					if m.autostart.Checked() {
						if err := autostart.Disable(); err != nil {
							showError("Could not disable start at login: " + err.Error())
						}
					} else {
						installAutostart(a)
					}
					if autostart.Enabled() {
						m.autostart.Check()
					} else {
						m.autostart.Uncheck()
					}
				})
			case <-m.log.ClickedCh:
				_ = openFile(a.LogPath)
			case <-m.quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
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
	if st.Kind == agent.NeedsSetup {
		m.login.Disable()
		m.logout.Disable()
		m.pause.Disable()
	} else {
		m.login.Enable()
		m.logout.Enable()
		m.pause.Enable()
	}
}
