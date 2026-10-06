// Package logx provides a tiny size-capped log file, so a tool that runs
// for months never fills the disk.
package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// RotatingFile is an io.Writer that keeps at most two files: path and
// path.1, each up to MaxSize bytes.
type RotatingFile struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	f       *os.File
	size    int64
}

// Open opens (appending to) the log file at path.
func Open(path string, maxSize int64) (*RotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &RotatingFile{path: path, maxSize: maxSize}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

// Write implements io.Writer.
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.maxSize && r.size > 0 {
		r.f.Close()
		_ = os.Rename(r.path, r.path+".1") // best effort; on failure keep appending
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// Close closes the file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// Path returns the current log file path.
func (r *RotatingFile) Path() string { return r.path }

// New returns a text logger writing to w.
func New(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
