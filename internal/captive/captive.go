// Package captive controls the operating system's own captive-portal
// window. On macOS, joining campus Wi-Fi pops up a "Log in to network"
// window before autoportal has had a chance to sign in; it can be turned
// off system-wide.
package captive

import "errors"

// ErrCancelled means the user dismissed the password prompt.
var ErrCancelled = errors.New("cancelled")

// Supported reports whether this OS has a popup autoportal can control.
func Supported() bool { return supported }
