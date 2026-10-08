//go:build !darwin

package captive

import "errors"

const supported = false

// PopupDisabled reports whether the OS's captive-portal window is off.
func PopupDisabled() bool { return false }

// SetPopupDisabled is only available on macOS.
func SetPopupDisabled(bool) error { return errors.New("not supported on this system") }
