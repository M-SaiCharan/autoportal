// Package sophostest provides a fake Sophos captive portal for tests. Its
// responses are byte-for-byte copies of what a real SFOS portal returned.
package sophostest

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/M-SaiCharan/autoportal/internal/portal"
)

const (
	liveXML      = `<?xml version='1.0' ?><requestresponse><status><![CDATA[LIVE]]></status><message><![CDATA[You are signed in as {username}]]></message><logoutmessage><![CDATA[You have successfully logged off]]></logoutmessage><state><![CDATA[]]></state><user><![CDATA[]]></user></requestresponse>`
	rejectXML    = `<?xml version='1.0' ?><requestresponse><status><![CDATA[LOGIN]]></status><message><![CDATA[Invalid user name/password. Please contact the administrator.]]></message></requestresponse>`
	challengeXML = `<?xml version='1.0' ?><requestresponse><status><![CDATA[CHALLENGE]]></status><message><![CDATA[Enter the OTP]]></message><state><![CDATA[abc123]]></state></requestresponse>`
	logoutXML    = `<?xml version='1.0' ?><requestresponse><status><![CDATA[LOGIN]]></status><message><![CDATA[You&#39;ve signed out]]></message></requestresponse>`

	// Page is a trimmed httpclient.html with the markers Detect looks for.
	Page = `<html><head><script language="JavaScript" src="/javascript/cyberoamAjax.js"></script>
<script type='text/javascript'>
    var title = 'Sign in to access the Test network';
    var keepaliverequest = 'N';
</script>
<script type='text/javascript' src='/javascript/validation/httpclient.js?ver=37594'></script>
</head><body onload='setup()'></body></html>`
)

// Server is a fake portal. Users maps username to password; a user named
// "otp" always gets a CHALLENGE.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	users    map[string]string
	loggedIn map[string]bool
	logins   int
	lastForm map[string]string
}

// NewTLS starts an HTTPS fake portal and registers cleanup with t.
func NewTLS(t testing.TB, users map[string]string) *Server {
	s := &Server{users: users, loggedIn: map[string]bool{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// Endpoint returns a pinned endpoint for this server.
func (s *Server) Endpoint(t testing.TB) *portal.Endpoint {
	t.Helper()
	base, err := portal.ParseEndpoint(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &portal.Endpoint{Base: base, Fingerprint: portal.Fingerprint(s.Certificate())}
}

// LoggedIn reports whether user currently has a session.
func (s *Server) LoggedIn(user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loggedIn[user]
}

// Logins returns how many login requests were received.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// LastForm returns the fields of the most recent POST.
func (s *Server) LastForm() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastForm
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/httpclient.html" {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, _ = w.Write([]byte(Page))
		return
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		http.Error(w, "bad content type", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	form := map[string]string{}
	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}
	if _, err := strconv.ParseInt(form["a"], 10, 64); err != nil || form["producttype"] != "0" {
		http.Error(w, "missing a/producttype", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastForm = form
	w.Header().Set("Content-Type", "text/xml")
	// The portal's JS doubles single quotes; undo that to look the user up.
	user := strings.ReplaceAll(form["username"], "''", "'")

	switch {
	case r.URL.Path == "/login.xml" && form["mode"] == "191":
		s.logins++
		switch {
		case user == "otp":
			_, _ = w.Write([]byte(challengeXML))
		case s.users[user] != "" && s.users[user] == form["password"]:
			s.loggedIn[user] = true
			_, _ = w.Write([]byte(liveXML))
		default:
			_, _ = w.Write([]byte(rejectXML))
		}
	case r.URL.Path == "/logout.xml" && form["mode"] == "193":
		delete(s.loggedIn, user)
		_, _ = w.Write([]byte(logoutXML))
	default:
		http.NotFound(w, r)
	}
}
