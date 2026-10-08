// Package history remembers recent automatic logins, so the menu can say
// how often autoportal has signed the user back in.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Window is how far back logins are kept and counted.
const Window = 7 * 24 * time.Hour

// History is a small JSON file of login times. It is safe for concurrent use.
type History struct {
	path string

	mu     sync.Mutex
	logins []time.Time // oldest first, within Window
}

type file struct {
	Logins []time.Time `json:"logins"`
}

// Open loads the history at path; a missing or damaged file starts empty.
func Open(path string) *History {
	h := &History{path: path}
	if b, err := os.ReadFile(path); err == nil {
		var f file
		if json.Unmarshal(b, &f) == nil {
			h.logins = f.Logins
		}
	}
	return h
}

// Record adds a login at t and saves the file.
func (h *History) Record(t time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logins = append(h.logins, t.Round(0))
	h.prune(t)
	return h.save()
}

func (h *History) prune(now time.Time) {
	i := 0
	for i < len(h.logins) && now.Sub(h.logins[i]) > Window {
		i++
	}
	h.logins = h.logins[i:]
}

func (h *History) save() error {
	b, err := json.Marshal(file{h.logins})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

// Stats summarises the history.
type Stats struct {
	Last  time.Time // most recent login; zero if none
	Count int       // logins within Window
}

// Stats returns the summary as of now.
func (h *History) Stats(now time.Time) Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	var s Stats
	for _, t := range h.logins {
		if now.Sub(t) <= Window {
			s.Count++
		}
	}
	if n := len(h.logins); n > 0 {
		s.Last = h.logins[n-1]
	}
	return s
}

// Line is the menu text, e.g. "Last auto-login 5 min ago · 12 this week".
func (s Stats) Line(now time.Time) string {
	if s.Last.IsZero() {
		return "No automatic logins yet"
	}
	line := "Last auto-login " + Ago(s.Last, now)
	if s.Count > 0 {
		line += fmt.Sprintf(" · %d this week", s.Count)
	}
	return line
}

// Ago formats t relative to now in a friendly way.
func Ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour && t.Day() == now.Day():
		return "today at " + t.Format("15:04")
	case d < 48*time.Hour && t.Day() == now.AddDate(0, 0, -1).Day():
		return "yesterday at " + t.Format("15:04")
	default:
		return t.Format("Mon 2 Jan, 15:04")
	}
}

// Remove deletes the history file.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
