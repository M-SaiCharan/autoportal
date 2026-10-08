package tray

import (
	"context"
	"fmt"
	"strings"

	"github.com/ncruces/zenity"

	"github.com/M-SaiCharan/autoportal/internal/app"
	"github.com/M-SaiCharan/autoportal/internal/autostart"
	"github.com/M-SaiCharan/autoportal/internal/portal"
	"github.com/M-SaiCharan/autoportal/internal/setup"
	"github.com/M-SaiCharan/autoportal/internal/store"
)

var dlgTitle = zenity.Title("autoportal")

func showError(msg string) { _ = zenity.Error(msg, dlgTitle) }

func showInfo(msg string) { _ = zenity.Info(msg, dlgTitle) }

// runSetup walks the user through setup with native dialogs. It returns
// quietly if the user cancels at any point.
func runSetup(ctx context.Context, a *app.App) {
	if !zenity.IsAvailable() {
		Notify("autoportal setup", "Open a terminal and run: autoportal setup")
		return
	}
	prev := a.Config()

	p := choosePortal(ctx, prev)
	if p == nil {
		return
	}
	if prev != nil && prev.Portal.Fingerprint != "" && p.Fingerprint != prev.Portal.Fingerprint &&
		strings.EqualFold(prev.Portal.URL, p.Base.String()) {
		msg := fmt.Sprintf("The portal's security certificate is different from the one saved earlier.\n\n"+
			"Only continue if you are on your campus network and your college renewed its certificate.\n\n"+
			"Saved:\n%s\n\nNow:\n%s", prev.Portal.Fingerprint, p.Fingerprint)
		if zenity.Question(msg, dlgTitle, zenity.WarningIcon,
			zenity.OKLabel("Trust new certificate"), zenity.CancelLabel("Cancel"), zenity.DefaultCancel()) != nil {
			return
		}
	}

	if !askCredentials(ctx, a, p, prev) {
		return
	}

	if prev == nil && !autostart.Enabled() {
		err := zenity.Question("You're signed in!\n\nStart autoportal automatically when you log in to this computer? "+
			"(Recommended. Otherwise you'll have to open it yourself after every restart.)",
			dlgTitle, zenity.OKLabel("Yes, start automatically"), zenity.CancelLabel("Not now"))
		if err == nil {
			installAutostart(a)
		}
	}
}

// choosePortal finds the portal automatically or asks for its address.
func choosePortal(ctx context.Context, prev *store.Config) *setup.Portal {
	guess := ""
	if prev != nil {
		guess = prev.Portal.URL
	} else if found, err := setup.Discover(ctx); err == nil {
		msg := fmt.Sprintf("Found your network's login portal:\n\n%s\n%s\n\nUse it?", found.Info.Title, found.Base)
		if zenity.Question(msg, dlgTitle, zenity.OKLabel("Use it"), zenity.CancelLabel("Enter address…")) == nil {
			return found
		}
		guess = found.Base.String()
	}
	for {
		s, err := zenity.Entry("Address of your network's login page.\n\n"+
			"Tip: open the login page in a browser and copy what's in the address bar, "+
			"for example https://10.10.10.2:8090/httpclient.html",
			dlgTitle, zenity.EntryText(guess), zenity.OKLabel("Next"))
		if err != nil {
			return nil // cancelled
		}
		guess = s
		p, err := setup.Inspect(ctx, s)
		if err != nil {
			showError(setup.Friendly(err))
			continue
		}
		return p
	}
}

// askCredentials loops until a login succeeds (then saves) or the user
// cancels. It returns whether setup completed.
func askCredentials(ctx context.Context, a *app.App, p *setup.Portal, prev *store.Config) bool {
	prompt := "Sign in to " + p.Info.Title
	for {
		user, pass, err := zenity.Password(dlgTitle, zenity.Username(), zenity.OKLabel("Sign in"))
		if err != nil {
			return false
		}
		user = strings.TrimSpace(user)
		if user == "" || pass == "" {
			showError("Please enter both your username and your password.")
			continue
		}
		if _, err := setup.Verify(ctx, p, user, pass); err != nil {
			if portal.IsRejected(err) {
				showError(prompt + " failed: the portal rejected the username or password. Please try again.")
			} else {
				showError(setup.Friendly(err))
			}
			continue
		}
		if _, err := setup.Save(p, user, pass, prev); err != nil {
			showError("Could not save your settings: " + err.Error())
			return false
		}
		if err := a.Reload(ctx); err != nil {
			showError("Saved, but could not apply the new settings: " + err.Error())
			return false
		}
		return true
	}
}

// installAutostart copies the binary to its per-user home (if needed) and
// registers it to start at login.
func installAutostart(a *app.App) bool {
	path, err := autostart.Install()
	if err != nil {
		a.Log.Warn("autostart install failed", "err", err)
		showError("Could not enable start at login: " + err.Error())
		return false
	}
	a.Log.Info("autostart enabled", "path", path)
	return true
}
