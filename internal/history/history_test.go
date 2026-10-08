package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndStats(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.json")
	now := time.Date(2026, 10, 8, 14, 0, 0, 0, time.Local)
	h := Open(p)
	if s := h.Stats(now); s.Count != 0 || !s.Last.IsZero() {
		t.Fatalf("empty: %+v", s)
	}
	if got := h.Stats(now).Line(now); got != "No automatic logins yet" {
		t.Fatal(got)
	}
	h.Record(now.Add(-8 * 24 * time.Hour)) // pruned by the next record
	h.Record(now.Add(-2 * time.Hour))
	h.Record(now.Add(-5 * time.Minute))

	h2 := Open(p) // survives a restart
	s := h2.Stats(now)
	if s.Count != 2 || !s.Last.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("stats: %+v", s)
	}
	if got := s.Line(now); got != "Last auto-login 5 min ago · 2 this week" {
		t.Fatal(got)
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 8, 14, 0, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now",
		59 * time.Minute: "59 min ago",
		3 * time.Hour:    "today at 11:00",
		16 * time.Hour:   "yesterday at 22:00",
		72 * time.Hour:   "Mon 5 Oct, 14:00",
	} {
		if got := Ago(now.Add(-d), now); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

func TestDamagedFileStartsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.json")
	if err := writeString(p, "{not json"); err != nil {
		t.Fatal(err)
	}
	if s := Open(p).Stats(time.Now()); s.Count != 0 {
		t.Fatal(s)
	}
}

func writeString(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }
