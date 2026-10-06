package netwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbe(t *testing.T) {
	ok204 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ok204.Close()
	// What the Sophos web proxy answers for a logged-out client.
	proxy403 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Via", "HTTP/1.1 forward.http.proxy:3128")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxy403.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://10.0.0.1:8090/httpclient.html", http.StatusFound)
	}))
	defer redirect.Close()
	wrongBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>login here</html>"))
	}))
	defer wrongBody.Close()
	dead := "http://127.0.0.1:1/" // nothing listens on port 1

	cases := []struct {
		name    string
		targets []Target
		want    Connectivity
	}{
		{"online", []Target{{URL: ok204.URL, Status: 204}}, Online},
		{"sophos 403", []Target{{URL: proxy403.URL, Status: 204}}, Blocked},
		{"redirect", []Target{{URL: redirect.URL, Status: 204}}, Blocked},
		{"wrong body", []Target{{URL: wrongBody.URL, Status: 200, Contains: "Success"}}, Blocked},
		{"offline", []Target{{URL: dead, Status: 204}}, Offline},
		{"one good is enough", []Target{{URL: dead, Status: 204}, {URL: proxy403.URL, Status: 204}, {URL: ok204.URL, Status: 204}}, Online},
	}
	for _, c := range cases {
		p := &Prober{Targets: c.targets, Timeout: 2 * time.Second}
		if got := p.Probe(context.Background()); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWatcherStep(t *testing.T) {
	addrs := "en0=10.0.0.5"
	w := &Watcher{Interval: 3 * time.Second, Addrs: func() string { return addrs }}
	sig := addrs

	if _, ok := w.step(3*time.Second, &sig); ok {
		t.Fatal("event on a quiet tick")
	}
	if ev, ok := w.step(10*time.Minute, &sig); !ok || ev.Reason != "wake" {
		t.Fatalf("long gap: %v %v, want wake", ev, ok)
	}
	addrs = "en0=10.0.0.9"
	if ev, ok := w.step(3*time.Second, &sig); !ok || ev.Reason != "network-change" {
		t.Fatalf("address change: %v %v", ev, ok)
	}
	if _, ok := w.step(3*time.Second, &sig); ok {
		t.Fatal("event repeated without a new change")
	}
}

func TestInterfaceSignatureStable(t *testing.T) {
	if a, b := InterfaceSignature(), InterfaceSignature(); a != b {
		t.Fatalf("signature not stable: %q vs %q", a, b)
	}
}
