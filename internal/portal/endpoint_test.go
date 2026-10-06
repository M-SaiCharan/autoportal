package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseEndpoint(t *testing.T) {
	cases := map[string]string{
		"10.10.10.2":        "https://10.10.10.2:8090",
		" 10.10.10.2:8090 ": "https://10.10.10.2:8090",
		"https://10.10.10.2:8090/httpclient.html": "https://10.10.10.2:8090",
		"http://portal.example.edu:1000/x?y=1":    "http://portal.example.edu:1000",
		"https://portal.example.edu":              "https://portal.example.edu",
		"portal.example.edu":                      "https://portal.example.edu:8090",
	}
	for in, want := range cases {
		u, err := ParseEndpoint(in)
		if err != nil {
			t.Errorf("ParseEndpoint(%q): %v", in, err)
			continue
		}
		if u.String() != want {
			t.Errorf("ParseEndpoint(%q) = %q, want %q", in, u, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "https://"} {
		if _, err := ParseEndpoint(bad); err == nil {
			t.Errorf("ParseEndpoint(%q) succeeded", bad)
		}
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	const canon = "44:86:F6:28:81:1F:A9:77:5D:8E:6E:CD:5F:DF:11:DF:0A:E3:C8:9E:CB:62:2F:FC:14:E1:F7:64:0F:18:36:6C"
	for _, in := range []string{
		canon,
		"sha256 Fingerprint=" + canon,
		"4486f628811fa9775d8e6ecd5fdf11df0ae3c89ecb622ffc14e1f7640f18366c",
	} {
		got, err := NormalizeFingerprint(in)
		if err != nil || got != canon {
			t.Errorf("NormalizeFingerprint(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeFingerprint("44:86"); err == nil {
		t.Error("short fingerprint accepted")
	}
}

func TestPinnedTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	base, _ := ParseEndpoint(srv.URL)
	ctx := context.Background()

	cert, err := FetchCertificate(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	good := &Endpoint{Base: base, Fingerprint: Fingerprint(cert)}
	if err := good.Reachable(ctx); err != nil {
		t.Fatalf("Reachable with correct pin: %v", err)
	}

	bad := &Endpoint{Base: base, Fingerprint: "AA" + good.Fingerprint[2:]}
	var mm *CertMismatchError
	if err := bad.Reachable(ctx); !errors.As(err, &mm) || mm.Got != good.Fingerprint {
		t.Fatalf("Reachable with wrong pin: %v", err)
	}

	unpinned := &Endpoint{Base: base}
	if err := unpinned.Reachable(ctx); !errors.Is(err, ErrNotPinned) {
		t.Fatalf("Reachable without pin: %v", err)
	}
}
