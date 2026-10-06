// Package instance makes sure only one autoportal agent runs per user.
package instance

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrRunning means another instance holds the lock.
var ErrRunning = errors.New("autoportal is already running")

// Lock is held for the life of the process; the OS releases it if the
// process dies, so a crash never leaves a stale lock behind.
type Lock struct{ f *os.File }

// Acquire takes the lock file in dir, or returns ErrRunning.
func Acquire(dir string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "autoportal.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, ErrRunning
	}
	return &Lock{f: f}, nil
}

// Release frees the lock.
func (l *Lock) Release() error { return l.f.Close() }
