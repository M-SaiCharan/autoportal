// Package autostart registers autoportal to start at user login and can
// copy the running binary to a stable per-user location. Nothing here
// needs administrator rights.
package autostart

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Executable returns the absolute, symlink-resolved path of this binary.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Abs(exe)
}

// InstallPath is where Install copies the binary. It is empty when the
// binary should stay where it is (a macOS .app bundle).
func InstallPath() (string, error) {
	exe, err := Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" && strings.Contains(exe, ".app/Contents/MacOS/") {
		return "", nil
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
		return filepath.Join(base, "Programs", "autoportal", "autoportal.exe"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "autoportal"), nil
}

// Install copies the running binary to InstallPath (if needed) and
// enables start-at-login for the installed copy. It returns the path
// that will be started.
func Install() (string, error) {
	exe, err := Executable()
	if err != nil {
		return "", err
	}
	if strings.Contains(exe, "/AppTranslocation/") {
		return "", errors.New("macOS is running autoportal from a temporary location: " +
			"move autoportal.app into your Applications folder, open it from there, and try again")
	}
	target, err := InstallPath()
	if err != nil {
		return "", err
	}
	if target == "" {
		target = exe
	} else if !samePath(exe, target) {
		if err := copyBinary(exe, target); err != nil {
			return "", fmt.Errorf("copying to %s: %w", target, err)
		}
	}
	if err := Enable(target); err != nil {
		return "", err
	}
	return target, nil
}

// Uninstall disables start-at-login and removes the installed copy if this
// process is not running from it.
func Uninstall() error {
	if err := Disable(); err != nil {
		return err
	}
	removeExtras()
	target, err := InstallPath()
	if err != nil || target == "" {
		return err
	}
	exe, _ := Executable()
	if samePath(exe, target) {
		return nil // can't delete ourselves on every OS; leave it
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("start at login is off, but %s could not be deleted "+
			"(quit autoportal from its menu first): %w", target, err)
	}
	_ = os.Remove(target + ".old")
	return nil
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// copyBinary writes src to dst via a temporary file and a rename, which
// also works while dst is running (on Windows the running file is moved
// aside first).
func copyBinary(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".autoportal-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(dst + ".old")
		if err := os.Rename(dst, dst+".old"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tmp.Name(), dst)
}
