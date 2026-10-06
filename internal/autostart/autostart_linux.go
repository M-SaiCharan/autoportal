//go:build linux

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// XDG autostart entry, honoured by GNOME, KDE, XFCE, Cinnamon, MATE...
func entryPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "autoportal.desktop"), nil
}

func desktopEntry(exe string) string {
	// Desktop-entry Exec quoting: wrap in double quotes, escape " ` $ \.
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(exe)
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=autoportal
Comment=Automatic captive-portal login
Exec="%s" tray
Icon=network-wireless
Terminal=false
X-GNOME-Autostart-enabled=true
X-GNOME-Autostart-Delay=2
`, q)
}

// Enable starts exe at login.
func Enable(exe string) error {
	p, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(desktopEntry(exe)), 0o644)
}

// Disable removes the login item.
func Disable() error {
	p, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Enabled reports whether a login item exists.
func Enabled() bool {
	p, err := entryPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
