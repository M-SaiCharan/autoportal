//go:build darwin

package captive

import (
	"errors"
	"os/exec"
	"strings"
)

const supported = true

const domain = "/Library/Preferences/SystemConfiguration/com.apple.captive.control"

// PopupDisabled reports whether macOS's captive-portal window is off.
func PopupDisabled() bool {
	out, err := exec.Command("/usr/bin/defaults", "read", domain, "Active").Output()
	if err != nil {
		return false // key absent: the popup is on (the default)
	}
	return strings.TrimSpace(string(out)) == "0"
}

// SetPopupDisabled turns the window off or back on. It changes a system
// setting, so macOS asks for an administrator password.
func SetPopupDisabled(off bool) error {
	cmd := "/usr/bin/defaults delete " + domain + " Active"
	prompt := "autoportal wants to turn macOS's Wi-Fi login window back on."
	if off {
		cmd = "/usr/bin/defaults write " + domain + " Active -bool false"
		prompt = "autoportal wants to turn off macOS's Wi-Fi login window, so it can sign you in without it."
	}
	script := `do shell script "` + cmd + `" with prompt "` + prompt + `" with administrator privileges`
	out, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "-128") { // user cancelled
			return ErrCancelled
		}
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}
