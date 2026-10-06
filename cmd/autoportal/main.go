// Command autoportal keeps you signed in to a captive portal (Sophos
// Firewall / Cyberoam) by logging in automatically after every wake-up
// and network change.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/M-SaiCharan/autoportal/internal/app"
	"github.com/M-SaiCharan/autoportal/internal/tray"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `autoportal: automatic captive-portal login (Sophos Firewall / Cyberoam)

Usage:
  autoportal                 start the tray / menu-bar app (default)
  autoportal setup           set up the portal and your credentials in the terminal
  autoportal status          show whether you are signed in
  autoportal login           log in once, right now
  autoportal logout          log out once, right now
  autoportal install         start automatically at login (copies itself to a per-user folder)
  autoportal uninstall       stop starting at login [--purge also deletes settings and password]
  autoportal run             run without a tray icon, logging to the terminal (servers, debugging)
  autoportal version         print the version

Setup options (for scripts):
  autoportal setup --portal https://10.10.10.2:8090 --user NAME [--password-stdin] [--no-autostart]
`

func main() {
	args := os.Args[1:]
	cmd := "tray"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "tray":
		err = runTray(args)
	case "run":
		err = runHeadless(args)
	case "setup":
		err = cmdSetup(args)
	case "status":
		err = cmdStatus()
	case "login":
		err = cmdLogin()
	case "logout":
		err = cmdLogout()
	case "install":
		err = cmdInstall()
	case "uninstall":
		err = cmdUninstall(args)
	case "version", "--version", "-v":
		fmt.Println("autoportal", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "autoportal:", err)
		os.Exit(1)
	}
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func runTray(args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := app.Start(ctx, app.Options{Notify: tray.Notify, Debug: hasFlag(args, "--debug")})
	if errors.Is(err, app.ErrRunning) {
		tray.Notify("autoportal is already running", "Look for its icon in the menu bar / system tray.")
		return nil
	}
	if err != nil {
		tray.Notify("autoportal could not start", err.Error())
		return err
	}
	defer a.Close()
	tray.Run(ctx, cancel, a, version)
	return nil
}

func runHeadless(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := app.Start(ctx, app.Options{Stderr: true, Debug: hasFlag(args, "--debug")})
	if err != nil {
		return err
	}
	defer a.Close()
	if a.Config() == nil {
		return errors.New("not set up yet; run: autoportal setup")
	}
	<-ctx.Done()
	return nil
}
