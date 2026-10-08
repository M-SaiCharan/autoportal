package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/M-SaiCharan/autoportal/internal/netwatch"
	_ "github.com/M-SaiCharan/autoportal/internal/portal/sophos"
	"github.com/M-SaiCharan/autoportal/internal/portal/sophos/sophostest"
	"github.com/M-SaiCharan/autoportal/internal/setup"
)

type prober netwatch.Connectivity

func (p prober) Probe(context.Context) netwatch.Connectivity { return netwatch.Connectivity(p) }

func TestReport(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("AUTOPORTAL_CONFIG_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx := context.Background()

	// Not set up.
	r := Run(ctx, Inputs{Version: "v0.2.0", Prober: prober(netwatch.Blocked)})
	if r.Problems() == 0 || !strings.Contains(r.String(), "not set up yet") {
		t.Fatalf("unconfigured report:\n%s", r)
	}

	srv := sophostest.NewTLS(t, map[string]string{"student": "s3cret-pw"})
	p, err := setup.Inspect(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Save(p, "student", "s3cret-pw", nil); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "autoportal.log")
	var lines []string
	for i := range 100 {
		lines = append(lines, "line "+string(rune('a'+i%26)))
	}
	os.WriteFile(log, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	r = Run(ctx, Inputs{Version: "v0.2.0", LogPath: log, InApp: true, Prober: prober(netwatch.Online),
		Now: func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }})
	out := r.String()
	for _, want := range []string{
		"[ OK ] Settings", "user student",
		"[ OK ] Portal          reachable, certificate matches",
		"[ OK ] Password", "[ OK ] Internet", "[WARN] Start at login",
		"--- recent log ---",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "s3cret-pw") {
		t.Fatal("report contains the password")
	}
	if len(r.LogTail) != 40 {
		t.Fatalf("log tail has %d lines", len(r.LogTail))
	}
	if r.Problems() != 0 {
		t.Fatalf("unexpected failures:\n%s", out)
	}
}
