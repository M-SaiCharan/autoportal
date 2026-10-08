package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGitHub serves /<repo>/releases/latest and release downloads.
func fakeGitHub(t *testing.T, tag string, files map[string][]byte, sums string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/o/r/releases/latest":
			http.Redirect(w, r, "/o/r/releases/tag/"+tag, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/o/r/releases/download/"+tag+"/"):
			name := strings.TrimPrefix(r.URL.Path, "/o/r/releases/download/"+tag+"/")
			if name == "SHA256SUMS.txt" {
				fmt.Fprint(w, sums)
				return
			}
			b, ok := files[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sumsFor(files map[string][]byte) string {
	var b strings.Builder
	for name, data := range files {
		h := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	return b.String()
}

func TestParseVersion(t *testing.T) {
	for s, ok := range map[string]bool{
		"v0.1.0": true, "v10.2.33": true,
		"dev": false, "0.1.0": false, "v0.1": false, "v0.1.0-3-gabc": false,
		"v0.1.0-dirty": false, "v01.1.0": false, "abc1234": false,
	} {
		if _, got := parseVersion(s); got != ok {
			t.Errorf("parseVersion(%q) ok=%v, want %v", s, got, ok)
		}
	}
	a, _ := parseVersion("v0.10.0")
	b, _ := parseVersion("v0.9.9")
	if !a.newer(b) || b.newer(a) || a.newer(a) {
		t.Error("version ordering")
	}
}

func TestCheck(t *testing.T) {
	srv := fakeGitHub(t, "v0.3.0", nil, "")
	u := &Updater{Current: "v0.2.0", BaseURL: srv.URL, Repo: "o/r"}
	if tag, err := u.Check(context.Background()); err != nil || tag != "v0.3.0" {
		t.Fatalf("Check = %q, %v", tag, err)
	}
	u.Current = "v0.3.0"
	if tag, err := u.Check(context.Background()); err != nil || tag != "" {
		t.Fatalf("up to date: Check = %q, %v", tag, err)
	}
	u.Current = "v0.4.0"
	if tag, _ := u.Check(context.Background()); tag != "" {
		t.Fatalf("never downgrade: Check = %q", tag)
	}
	u.Current = "dev"
	if _, err := u.Check(context.Background()); err != ErrDevBuild {
		t.Fatalf("dev build: %v", err)
	}
}

func TestApplyBinary(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS updates the .app bundle; see TestReplaceBundle")
	}
	name, err := (&Updater{}).asset()
	if err != nil {
		t.Skip(err)
	}
	files := map[string][]byte{name: []byte("new binary")}
	srv := fakeGitHub(t, "v0.3.0", files, sumsFor(files))

	exe := filepath.Join(t.TempDir(), "autoportal")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	u := &Updater{Current: "v0.2.0", Exe: exe, BaseURL: srv.URL, Repo: "o/r"}
	if err := u.Apply(context.Background(), "v0.3.0"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Fatalf("exe = %q", b)
	}
	if fi, _ := os.Stat(exe); runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
		t.Fatal("new binary is not executable")
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old left behind")
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("binary asset only")
	}
	name, err := (&Updater{}).asset()
	if err != nil {
		t.Skip(err)
	}
	files := map[string][]byte{name: []byte("tampered")}
	srv := fakeGitHub(t, "v0.3.0", files, sumsFor(map[string][]byte{name: []byte("original")}))
	exe := filepath.Join(t.TempDir(), "autoportal")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	u := &Updater{Current: "v0.2.0", Exe: exe, BaseURL: srv.URL, Repo: "o/r"}
	if err := u.Apply(context.Background(), "v0.3.0"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Apply = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old binary" {
		t.Fatal("binary was replaced despite the bad checksum")
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		if strings.HasSuffix(name, "/") {
			h.SetMode(os.ModeDir | 0o755)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return b.Bytes()
}

func TestReplaceBundle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("app bundles are macOS-only")
	}
	dir := t.TempDir()
	app := filepath.Join(dir, "autoportal.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "autoportal"), []byte("old"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "stale"), []byte("x"), 0o644)

	z := makeZip(t, map[string]string{
		"autoportal.app/":                          "",
		"autoportal.app/Contents/Info.plist":       "plist",
		"autoportal.app/Contents/MacOS/autoportal": "new",
	})
	if err := replaceBundle(app, z); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "autoportal")); string(b) != "new" {
		t.Fatalf("binary = %q", b)
	}
	if _, err := os.Stat(filepath.Join(app, "Contents", "stale")); !os.IsNotExist(err) {
		t.Fatal("old bundle contents survived")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftovers next to the app: %v", entries)
	}

	// A zip without the app must leave the old bundle alone.
	if err := replaceBundle(app, makeZip(t, map[string]string{"other/file": "x"})); err == nil {
		t.Fatal("bogus zip accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "autoportal")); string(b) != "new" {
		t.Fatal("bundle damaged by a failed update")
	}
}

func TestUnzipRejectsEscapes(t *testing.T) {
	for _, name := range []string{"../evil", "/abs/evil", "a/../../evil"} {
		if err := unzip(makeZip(t, map[string]string{name: "x"}), t.TempDir()); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestBundle(t *testing.T) {
	if got := bundle("/Users/x/Applications/autoportal.app/Contents/MacOS/autoportal"); got != "/Users/x/Applications/autoportal.app" {
		t.Fatal(got)
	}
	if bundle("/usr/local/bin/autoportal") != "" {
		t.Fatal("not a bundle")
	}
}
