package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/M-SaiCharan/autoportal/internal/autostart"
	"github.com/M-SaiCharan/autoportal/internal/instance"
	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	"github.com/M-SaiCharan/autoportal/internal/portal"
	"github.com/M-SaiCharan/autoportal/internal/setup"
	"github.com/M-SaiCharan/autoportal/internal/store"

	_ "github.com/M-SaiCharan/autoportal/internal/portal/sophos" // register driver
)

var stdin = bufio.NewReader(os.Stdin)

func prompt(label, def string) (string, error) {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := stdin.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func confirm(label string, def bool) (bool, error) {
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	s, err := prompt(label+" ("+hint+")", "")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(s) {
	case "":
		return def, nil
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func readPassword(fromStdin bool) (string, error) {
	if fromStdin {
		line, err := stdin.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no terminal to read the password from; use --password-stdin")
	}
	fmt.Print("Password (hidden): ")
	b, err := term.ReadPassword(fd)
	fmt.Println()
	return string(b), err
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	portalAddr := fs.String("portal", "", "portal address, e.g. https://10.10.10.2:8090")
	user := fs.String("user", "", "username")
	pwStdin := fs.Bool("password-stdin", false, "read the password from standard input")
	noAuto := fs.Bool("no-autostart", false, "don't offer to start at login")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	prev, err := store.Load()
	if err != nil && !errors.Is(err, store.ErrNotConfigured) {
		return err
	}
	if errors.Is(err, store.ErrNotConfigured) {
		prev = nil
	}
	interactive := *portalAddr == "" || *user == ""

	// 1. Portal.
	var p *setup.Portal
	addr := *portalAddr
	if addr == "" && prev != nil {
		addr, err = prompt("Portal address", prev.Portal.URL)
		if err != nil {
			return err
		}
	}
	if addr == "" {
		fmt.Println("Looking for the login portal…")
		if found, err := setup.Discover(ctx); err == nil {
			fmt.Printf("Found %q at %s\n", found.Info.Title, found.Base)
			if ok, err := confirm("Use this portal?", true); err != nil {
				return err
			} else if ok {
				p = found
			}
		} else {
			fmt.Println("Not found automatically. Open the login page in a browser and copy its address.")
		}
	}
	for p == nil {
		if addr == "" {
			if addr, err = prompt("Portal address (e.g. https://10.10.10.2:8090)", ""); err != nil {
				return err
			}
			if addr == "" {
				continue
			}
		}
		p, err = setup.Inspect(ctx, addr)
		if err != nil {
			if !interactive {
				return errors.New(setup.Friendly(err))
			}
			fmt.Println("✗", setup.Friendly(err))
			addr = ""
		}
	}
	fmt.Printf("Portal:      %s (%s, %s)\n", p.Base, p.Info.Title, p.Info.Driver)
	if p.Fingerprint != "" {
		fmt.Printf("Certificate: %s\n             SHA-256 %s\n", p.CertSubject, p.Fingerprint)
	} else {
		fmt.Println("Warning:     this portal uses plain HTTP, so your password is sent unencrypted.")
	}
	if prev != nil && prev.Portal.Fingerprint != "" && p.Fingerprint != prev.Portal.Fingerprint &&
		strings.EqualFold(prev.Portal.URL, p.Base.String()) {
		fmt.Printf("\n⚠ The certificate changed since the last setup (was %s).\n"+
			"  Continue only if you are on your campus network and your college renewed it.\n", prev.Portal.Fingerprint)
		if ok, err := confirm("Trust the new certificate?", false); err != nil || !ok {
			return errors.New("setup cancelled")
		}
	}

	// 2. Credentials.
	for {
		u := *user
		if u == "" {
			def := ""
			if prev != nil {
				def = prev.Username
			}
			if u, err = prompt("Username", def); err != nil {
				return err
			}
		}
		pw, err := readPassword(*pwStdin)
		if err != nil {
			return err
		}
		if u == "" || pw == "" {
			fmt.Println("✗ Username and password are both required.")
			if !interactive {
				return errors.New("missing username or password")
			}
			continue
		}
		fmt.Print("Signing in… ")
		res, err := setup.Verify(ctx, p, u, pw)
		if err != nil {
			fmt.Println("✗", setup.Friendly(err))
			if !interactive || *pwStdin {
				return err
			}
			continue
		}
		fmt.Println("✓", res.Message)
		if _, err := setup.Save(p, u, pw, prev); err != nil {
			return err
		}
		fmt.Println("Saved. Your password is stored in the system keychain.")
		break
	}

	// 3. Autostart.
	if *noAuto || autostart.Enabled() {
		return nil
	}
	if interactive {
		ok, err := confirm("Start autoportal automatically when you log in?", true)
		if err != nil || !ok {
			return err
		}
		return cmdInstall()
	}
	return nil
}

func loadSession() (*store.Config, portal.Adapter, error) {
	cfg, err := store.Load()
	if errors.Is(err, store.ErrNotConfigured) {
		return nil, nil, errors.New("not set up yet; run: autoportal setup")
	}
	if err != nil {
		return nil, nil, err
	}
	ad, err := cfg.Adapter()
	return cfg, ad, err
}

func cmdLogin() error {
	cfg, ad, err := loadSession()
	if err != nil {
		return err
	}
	pw, err := store.GetPassword(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := ad.Login(ctx, cfg.Username, pw)
	if err != nil {
		return errors.New(setup.Friendly(err))
	}
	fmt.Println(res.Message)
	return nil
}

func cmdLogout() error {
	cfg, ad, err := loadSession()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := ad.Logout(ctx, cfg.Username)
	if err != nil {
		return errors.New(setup.Friendly(err))
	}
	fmt.Println(res.Message)
	return nil
}

func cmdStatus() error {
	cfg, ad, err := loadSession()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := netwatch.NewProber().Probe(ctx)
	reach := ad.Reachable(ctx)
	running := "no"
	if dir, err := store.StateDir(); err == nil {
		if l, err := instance.Acquire(dir); errors.Is(err, instance.ErrRunning) {
			running = "yes"
		} else if err == nil {
			l.Release()
		}
	}
	auto := "on"
	if cfg.Paused {
		auto = "paused"
	}
	startup := "no"
	if autostart.Enabled() {
		startup = "yes"
	}

	verdict := ""
	var mm *portal.CertMismatchError
	switch {
	case errors.As(reach, &mm):
		verdict = "⚠ Portal certificate changed; run setup again if your college renewed it."
	case reach != nil && conn == netwatch.Online:
		verdict = "Online, but not on the campus network."
	case reach != nil:
		verdict = "Portal not reachable: " + setup.Friendly(reach)
	case conn == netwatch.Online:
		verdict = "✓ Signed in as " + cfg.Username
	default:
		verdict = "✗ Not signed in (internet " + conn.String() + ")"
	}
	fmt.Println(verdict)
	fmt.Printf("  portal:     %s (%s)\n", cfg.Portal.URL, cfg.Portal.Title)
	fmt.Printf("  user:       %s\n", cfg.Username)
	fmt.Printf("  auto-login: %s · agent running: %s · starts at login: %s\n", auto, running, startup)
	return nil
}

func cmdInstall() error {
	path, err := autostart.Install()
	if err != nil {
		return err
	}
	fmt.Println("autoportal will start automatically at login:", path)
	return nil
}

func cmdUninstall(args []string) error {
	if err := autostart.Uninstall(); err != nil {
		return err
	}
	fmt.Println("autoportal will no longer start at login.")
	if !hasFlag(args, "--purge") {
		return nil
	}
	if cfg, err := store.Load(); err == nil {
		if err := store.DeletePassword(cfg); err != nil {
			return err
		}
	}
	if err := store.Remove(); err != nil {
		return err
	}
	fmt.Println("Settings and saved password deleted.")
	return nil
}
