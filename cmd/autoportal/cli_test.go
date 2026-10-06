package main

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/M-SaiCharan/autoportal/internal/portal/sophos/sophostest"
	"github.com/M-SaiCharan/autoportal/internal/store"
)

// captureStdout runs fn and returns what it printed.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

func TestCLISetupLoginLogout(t *testing.T) {
	keyring.MockInit()
	t.Setenv("AUTOPORTAL_CONFIG_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep autostart away from the real desktop
	t.Setenv("HOME", t.TempDir())            // and uninstall away from ~/.local/bin
	srv := sophostest.NewTLS(t, map[string]string{"student": "pw"})

	// Wrong password with --password-stdin must fail without saving.
	stdin = bufio.NewReader(strings.NewReader("nope\n"))
	_, err := captureStdout(t, func() error {
		return cmdSetup([]string{"--portal", srv.URL, "--user", "student", "--password-stdin", "--no-autostart"})
	})
	if err == nil {
		t.Fatal("setup with wrong password succeeded")
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("config saved despite failed login")
	}

	stdin = bufio.NewReader(strings.NewReader("pw\n"))
	out, err := captureStdout(t, func() error {
		return cmdSetup([]string{"--portal", srv.URL, "--user", "student", "--password-stdin", "--no-autostart"})
	})
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ You are signed in as student") || !strings.Contains(out, "SHA-256") {
		t.Fatalf("setup output:\n%s", out)
	}

	out, err = captureStdout(t, cmdLogout)
	if err != nil || !strings.Contains(out, "You've signed out") {
		t.Fatalf("logout: %v %q", err, out)
	}
	if srv.LoggedIn("student") {
		t.Fatal("server still has a session")
	}

	out, err = captureStdout(t, cmdLogin)
	if err != nil || !strings.Contains(out, "signed in as student") {
		t.Fatalf("login: %v %q", err, out)
	}

	out, err = captureStdout(t, cmdStatus)
	if err != nil || !strings.Contains(out, "user:       student") {
		t.Fatalf("status: %v %q", err, out)
	}

	if runtime.GOOS == "windows" {
		t.Log("skipping uninstall: it edits the real HKCU Run key on Windows")
		return
	}
	out, err = captureStdout(t, func() error { return cmdUninstall([]string{"--purge"}) })
	if err != nil || !strings.Contains(out, "Settings and saved password deleted") {
		t.Fatalf("uninstall: %v %q", err, out)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("config still present after purge")
	}
}
