//go:build !windows

package update

import (
	"os"
	"syscall"
)

// Restart replaces this process with exe, keeping the process ID, so a
// supervisor such as launchd still sees the same job.
func Restart(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}
