package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AUTOPORTAL_CONFIG_DIR", dir)

	if _, err := Load(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load on empty dir: %v", err)
	}
	in := &Config{
		Portal:   PortalConfig{Type: "sophos", URL: "https://10.10.10.2:8090", Fingerprint: "AA:BB", Title: "T"},
		Username: "student",
		Paused:   true,
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if *out != *in {
		t.Fatalf("round trip: got %+v, want %+v", out, in)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	if got := out.Account(); got != "student@10.10.10.2:8090" {
		t.Errorf("Account = %q", got)
	}
}

func TestPassword(t *testing.T) {
	keyring.MockInit()
	c := &Config{Portal: PortalConfig{URL: "10.0.0.1"}, Username: "u"}
	if _, err := GetPassword(c); err == nil {
		t.Fatal("expected error for missing password")
	}
	if err := SetPassword(c, "pw"); err != nil {
		t.Fatal(err)
	}
	if pw, err := GetPassword(c); err != nil || pw != "pw" {
		t.Fatalf("GetPassword = %q, %v", pw, err)
	}
	if err := DeletePassword(c); err != nil {
		t.Fatal(err)
	}
	if err := DeletePassword(c); err != nil {
		t.Fatalf("second delete should be a no-op: %v", err)
	}
}
