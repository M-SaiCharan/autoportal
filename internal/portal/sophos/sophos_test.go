package sophos_test

import (
	"context"
	"errors"
	"testing"

	"github.com/M-SaiCharan/autoportal/internal/portal"
	"github.com/M-SaiCharan/autoportal/internal/portal/sophos"
	"github.com/M-SaiCharan/autoportal/internal/portal/sophos/sophostest"
)

func TestLoginLogout(t *testing.T) {
	srv := sophostest.NewTLS(t, map[string]string{"student": "s3cret&=+"})
	c := sophos.New(srv.Endpoint(t))
	ctx := context.Background()

	res, err := c.Login(ctx, "student", "s3cret&=+")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.Message != "You are signed in as student" {
		t.Errorf("message = %q", res.Message)
	}
	if !srv.LoggedIn("student") {
		t.Fatal("server has no session")
	}
	f := srv.LastForm()
	if f["mode"] != "191" || f["password"] != "s3cret&=+" {
		t.Errorf("form = %v", f)
	}

	// Logging in again must be harmless (the real portal answers LIVE).
	if _, err := c.Login(ctx, "student", "s3cret&=+"); err != nil {
		t.Fatalf("second Login: %v", err)
	}

	res, err = c.Logout(ctx, "student")
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if res.Message != "You've signed out" {
		t.Errorf("logout message = %q (HTML entity not decoded?)", res.Message)
	}
	if srv.LoggedIn("student") {
		t.Error("still logged in after logout")
	}
}

func TestQuoteDoubling(t *testing.T) {
	srv := sophostest.NewTLS(t, map[string]string{"o'neil": "pw"})
	c := sophos.New(srv.Endpoint(t))
	if _, err := c.Login(context.Background(), "o'neil", "pw"); err != nil {
		t.Fatal(err)
	}
	if got := srv.LastForm()["username"]; got != "o''neil" {
		t.Errorf("username sent as %q, want o''neil", got)
	}
}

func TestRejected(t *testing.T) {
	srv := sophostest.NewTLS(t, map[string]string{"student": "right"})
	c := sophos.New(srv.Endpoint(t))
	_, err := c.Login(context.Background(), "student", "wrong")
	if !portal.IsRejected(err) {
		t.Fatalf("err = %v, want RejectedError", err)
	}
	if want := "login rejected: Invalid user name/password. Please contact the administrator."; err.Error() != want {
		t.Errorf("err = %q", err)
	}
}

func TestChallenge(t *testing.T) {
	srv := sophostest.NewTLS(t, nil)
	_, err := sophos.New(srv.Endpoint(t)).Login(context.Background(), "otp", "x")
	if !errors.Is(err, portal.ErrChallenge) {
		t.Fatalf("err = %v, want ErrChallenge", err)
	}
}

func TestCertificatePinRefusesImpostor(t *testing.T) {
	srv := sophostest.NewTLS(t, map[string]string{"student": "pw"})
	ep := srv.Endpoint(t)
	ep.Fingerprint = "00:" + ep.Fingerprint[3:] // a different certificate
	_, err := sophos.New(ep).Login(context.Background(), "student", "pw")
	var mm *portal.CertMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("err = %v, want CertMismatchError", err)
	}
	if srv.Logins() != 0 {
		t.Fatal("credentials were sent to a server with the wrong certificate")
	}
}

func TestDetect(t *testing.T) {
	srv := sophostest.NewTLS(t, nil)
	info, err := portal.Detect(context.Background(), srv.Endpoint(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Driver != "sophos" || info.Title != "Sign in to access the Test network" {
		t.Errorf("info = %+v", info)
	}
}
