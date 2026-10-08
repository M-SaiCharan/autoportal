//go:build windows

package update

import "os/exec"

// Restart starts exe again; the caller then exits.
func Restart(exe string, args []string) error {
	return exec.Command(exe, args...).Start()
}
