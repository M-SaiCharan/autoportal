//go:build linux

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/M-SaiCharan/autoportal/internal/icon"
)

// XDG autostart entry, honoured by GNOME, KDE, XFCE, Cinnamon, MATE...
func entryPath() (string, error) {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "autoportal.desktop"), nil
}

// The app-menu entry lets users start autoportal again after quitting it.
func menuEntryPath() (string, error) {
	dir, err := xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "applications", "autoportal.desktop"), nil
}

func iconPath() (string, error) {
	dir, err := xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "icons", "hicolor", "256x256", "apps", "autoportal.png"), nil
}

func xdgDir(env, fallback string) (string, error) {
	if d := os.Getenv(env); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}

func desktopEntry(exe string, autostart bool) string {
	// Desktop-entry Exec quoting: wrap in double quotes, escape " ` $ \.
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(exe)
	s := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=autoportal
GenericName=Wi-Fi login
Comment=Signs you in to the campus network automatically
Exec="%s" tray
Icon=autoportal
Terminal=false
Categories=Network;
Keywords=wifi;login;portal;sophos;captive;
`, q)
	if autostart {
		s += "X-GNOME-Autostart-enabled=true\nX-GNOME-Autostart-Delay=2\n"
	}
	return s
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// Enable starts exe at login. It also adds autoportal to the app menu,
// with its icon.
func Enable(exe string) error {
	p, err := entryPath()
	if err != nil {
		return err
	}
	if err := writeFile(p, []byte(desktopEntry(exe, true))); err != nil {
		return err
	}
	// The menu entry and icon are conveniences; failing them is not fatal.
	if ip, err := iconPath(); err == nil {
		_ = writeFile(ip, icon.PNG(icon.App(256)))
	}
	if mp, err := menuEntryPath(); err == nil {
		_ = writeFile(mp, []byte(desktopEntry(exe, false)))
	}
	return nil
}

// removeExtras deletes the app-menu entry and icon (uninstall).
func removeExtras() {
	if p, err := menuEntryPath(); err == nil {
		_ = os.Remove(p)
	}
	if p, err := iconPath(); err == nil {
		_ = os.Remove(p)
	}
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
