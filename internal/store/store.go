// Package store keeps autoportal's settings in a small JSON file and the
// password in the operating system's keychain (macOS Keychain, Windows
// Credential Manager, GNOME Keyring / KWallet via Secret Service).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"github.com/M-SaiCharan/autoportal/internal/portal"
)

// AppName names the config/cache folders and the keychain service.
const AppName = "autoportal"

// ErrNotConfigured means setup has not been run yet.
var ErrNotConfigured = errors.New("autoportal is not set up yet")

// Config is everything except the password.
type Config struct {
	Portal   PortalConfig `json:"portal"`
	Username string       `json:"username"`
	// Paused survives restarts so a deliberate logout stays logged out.
	Paused bool `json:"paused,omitempty"`
	// NotifyOnLogin shows a notification after each automatic login.
	// Off by default: the whole point is not to notice.
	NotifyOnLogin bool `json:"notify_on_login,omitempty"`
}

// PortalConfig identifies the captive portal.
type PortalConfig struct {
	Type        string `json:"type"`                  // driver name, e.g. "sophos"
	URL         string `json:"url"`                   // e.g. https://10.10.10.2:8090
	Fingerprint string `json:"fingerprint,omitempty"` // pinned SHA-256 of the HTTPS certificate
	Title       string `json:"title,omitempty"`       // portal's own title, for display
}

// Endpoint builds the pinned portal endpoint described by c.
func (c *Config) Endpoint() (*portal.Endpoint, error) {
	base, err := portal.ParseEndpoint(c.Portal.URL)
	if err != nil {
		return nil, err
	}
	return &portal.Endpoint{Base: base, Fingerprint: c.Portal.Fingerprint}, nil
}

// Adapter opens the configured portal driver.
func (c *Config) Adapter() (portal.Adapter, error) {
	ep, err := c.Endpoint()
	if err != nil {
		return nil, err
	}
	return portal.Open(c.Portal.Type, ep)
}

// Account is the keychain account name for this config's password.
func (c *Config) Account() string {
	host := c.Portal.URL
	if ep, err := c.Endpoint(); err == nil {
		host = ep.Base.Host
	}
	return c.Username + "@" + host
}

// ConfigDir returns the settings folder. AUTOPORTAL_CONFIG_DIR overrides it.
func ConfigDir() (string, error) {
	if d := os.Getenv("AUTOPORTAL_CONFIG_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, AppName), nil
}

// StateDir returns the folder for the log file and the instance lock.
func StateDir() (string, error) {
	if d := os.Getenv("AUTOPORTAL_CONFIG_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, AppName), nil
}

func configPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the config file; ErrNotConfigured if there is none.
func Load() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config file %s is damaged: %w", p, err)
	}
	if c.Portal.URL == "" || c.Username == "" {
		return nil, ErrNotConfigured
	}
	if c.Portal.Type == "" {
		c.Portal.Type = "sophos"
	}
	return &c, nil
}

// Save writes the config file atomically, readable only by the user.
func Save(c *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, fs.ErrPermission) {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Remove deletes the config file (used by uninstall).
func Remove() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// GetPassword reads the password for c from the keychain.
func GetPassword(c *Config) (string, error) {
	pw, err := keyring.Get(AppName, c.Account())
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("no saved password for %s; run setup", c.Username)
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the system keychain: %w", err)
	}
	return pw, nil
}

// SetPassword stores the password for c in the keychain.
func SetPassword(c *Config, password string) error {
	if err := keyring.Set(AppName, c.Account(), password); err != nil {
		return fmt.Errorf("cannot save to the system keychain: %w", err)
	}
	return nil
}

// DeletePassword removes the password for c from the keychain.
func DeletePassword(c *Config) error {
	err := keyring.Delete(AppName, c.Account())
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
