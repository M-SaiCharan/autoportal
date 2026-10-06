//go:build linux

package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableDisable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if Enabled() {
		t.Fatal("enabled before Enable")
	}
	if err := Enable(`/home/a b/$x/autoportal`); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("not enabled after Enable")
	}
	p, _ := entryPath()
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `Exec="/home/a b/\$x/autoportal" tray`) {
		t.Fatalf("bad Exec line:\n%s", b)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("still enabled after Disable")
	}
	if err := Disable(); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}

func TestCopyBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "bin", "autoportal")
	os.WriteFile(src, []byte("v1"), 0o755)
	if err := copyBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(src, []byte("v2"), 0o755)
	if err := copyBinary(src, dst); err != nil { // upgrade in place
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	fi, _ := os.Stat(dst)
	if string(b) != "v2" || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("dst = %q mode %v", b, fi.Mode())
	}
}
