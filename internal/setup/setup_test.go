package setup

import (
	"context"
	"testing"

	"github.com/zalando/go-keyring"

	_ "github.com/M-SaiCharan/autoportal/internal/portal/sophos"
	"github.com/M-SaiCharan/autoportal/internal/portal/sophos/sophostest"
	"github.com/M-SaiCharan/autoportal/internal/store"
)

func TestInspectVerifySave(t *testing.T) {
	keyring.MockInit()
	t.Setenv("AUTOPORTAL_CONFIG_DIR", t.TempDir())
	srv := sophostest.NewTLS(t, map[string]string{"student": "pw"})
	ctx := context.Background()

	p, err := Inspect(ctx, srv.URL+"/httpclient.html")
	if err != nil {
		t.Fatal(err)
	}
	if p.Info.Driver != "sophos" || p.Fingerprint == "" {
		t.Fatalf("portal = %+v", p)
	}

	if _, err := Verify(ctx, p, "student", "wrong"); Friendly(err) != "The portal rejected the username or password." {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := Verify(ctx, p, "student", "pw"); err != nil {
		t.Fatal(err)
	}

	c, err := Save(p, "student", "pw", nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if *loaded != *c {
		t.Fatalf("loaded %+v, saved %+v", loaded, c)
	}
	if pw, err := store.GetPassword(loaded); err != nil || pw != "pw" {
		t.Fatalf("password = %q, %v", pw, err)
	}

	// Changing the username moves the password and drops the old one.
	c2, err := Save(p, "other", "pw2", c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPassword(c); err == nil {
		t.Error("old account's password was not removed")
	}
	if pw, _ := store.GetPassword(c2); pw != "pw2" {
		t.Errorf("new password = %q", pw)
	}
}

func TestInspectNotAPortal(t *testing.T) {
	if _, err := Inspect(context.Background(), "127.0.0.1:1"); err == nil {
		t.Fatal("expected error for closed port")
	}
}
