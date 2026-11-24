package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
)

var (
	webToken = []byte(strings.Repeat("w", 32))
	apiToken = strings.Repeat("a", 32)
)

func newTestHandler(t *testing.T, now time.Time) *Handler {
	t.Helper()
	stateDir := t.TempDir()
	h := New(Options{
		Token:    webToken,
		Input:    config.Input{CWD: stateDir, UserConfigDir: t.TempDir(), StateDir: &stateDir},
		StateDir: stateDir,
		Version:  "test",
		Now:      func() time.Time { return now },
	})
	t.Cleanup(h.Shutdown)
	return h
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	h.ServeHTTP(response, r)
	return response
}

func loginRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func sessionCookie(t *testing.T, h *Handler, r *http.Request) *http.Cookie {
	t.Helper()
	response := serve(h, r)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == cookieName {
			return cookie
		}
	}
	t.Fatalf("login status=%d set no session cookie", response.Code)
	return nil
}

func assertSecurityHeaders(t *testing.T, label string, response *httptest.ResponseRecorder) {
	t.Helper()
	header := response.Header()
	if header.Get("Content-Security-Policy") != "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'" ||
		header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("%s: missing security headers: %v", label, header)
	}
}

func TestSessionsGuardPagesAndEvents(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	h := newTestHandler(t, now)
	valid := sessionCookie(t, h, loginRequest(string(webToken)))

	other := New(Options{Token: []byte(strings.Repeat("o", 32)), StateDir: t.TempDir(), Now: func() time.Time { return now }})
	t.Cleanup(other.Shutdown)
	otherCookie := sessionCookie(t, other, loginRequest(strings.Repeat("o", 32)))

	expired := &http.Cookie{Name: cookieName, Value: h.sign(now.Add(-time.Second))}
	expiry, mac, _ := strings.Cut(valid.Value, ".")
	tampered := &http.Cookie{Name: cookieName, Value: expiry + "9." + mac}

	for name, cookie := range map[string]*http.Cookie{"missing": nil, "tampered": tampered, "expired": expired, "other token": otherCookie} {
		for _, path := range []string{"/", "/projects/1", "/runs/1"} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			if cookie != nil {
				r.AddCookie(cookie)
			}
			response := serve(h, r)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
				t.Fatalf("%s cookie %s: status=%d location=%q", name, path, response.Code, response.Header().Get("Location"))
			}
		}
		r := httptest.NewRequest(http.MethodGet, "/events", nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if response := serve(h, r); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s cookie /events: status=%d", name, response.Code)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(valid)
	response := serve(h, r)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("signed-in overview: status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body)
	}
	assertSecurityHeaders(t, "overview", response)
}

func TestLogin(t *testing.T) {
	h := newTestHandler(t, time.Now())
	for name, token := range map[string]string{"wrong": strings.Repeat("x", 32), "API": apiToken, "empty": ""} {
		response := serve(h, loginRequest(token))
		if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
			t.Fatalf("%s token: status=%d cookies=%v", name, response.Code, response.Result().Cookies())
		}
		if strings.Contains(response.Body.String(), token) && token != "" {
			t.Fatalf("%s token echoed in the form", name)
		}
	}

	plain := serve(h, loginRequest(string(webToken)))
	cookie := sessionCookie(t, h, loginRequest(string(webToken)))
	if plain.Code != http.StatusSeeOther || plain.Header().Get("Location") != "/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/" {
		t.Fatalf("plain login: status=%d cookie=%+v", plain.Code, cookie)
	}
	if strings.Contains(cookie.Value, string(webToken)) {
		t.Fatal("session cookie holds the token")
	}

	proxied := loginRequest(string(webToken))
	proxied.Header.Set("X-Forwarded-Proto", "https")
	if cookie := sessionCookie(t, h, proxied); !cookie.Secure {
		t.Fatalf("proxied HTTPS login cookie is not Secure: %+v", cookie)
	}
	direct := loginRequest(string(webToken))
	direct.TLS = &tls.ConnectionState{}
	if cookie := sessionCookie(t, h, direct); !cookie.Secure {
		t.Fatalf("TLS login cookie is not Secure: %+v", cookie)
	}
}

func TestCrossSitePostIsRejected(t *testing.T) {
	h := newTestHandler(t, time.Now())
	for _, path := range []string{"/login", "/logout"} {
		r := loginRequest(string(webToken))
		r.URL.Path = path
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		if response := serve(h, r); response.Code != http.StatusForbidden || len(response.Result().Cookies()) != 0 {
			t.Fatalf("cross-site POST %s: status=%d", path, response.Code)
		}
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newTestHandler(t, time.Now())
	cookie := sessionCookie(t, h, loginRequest(string(webToken)))
	for _, r := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/login", nil),
		loginRequest("wrong"),
		httptest.NewRequest(http.MethodGet, "/", nil),
		httptest.NewRequest(http.MethodGet, "/nope", nil),
		httptest.NewRequest(http.MethodGet, "/assets/"+assetHash+"/app.css", nil),
		httptest.NewRequest(http.MethodGet, "/assets/stale/app.css", nil),
		httptest.NewRequest(http.MethodGet, "/events", nil),
	} {
		assertSecurityHeaders(t, r.Method+" "+r.URL.Path, serve(h, r))
	}
	signed := httptest.NewRequest(http.MethodGet, "/projects/7", nil)
	signed.AddCookie(cookie)
	response := serve(h, signed)
	if response.Code != http.StatusNotFound || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unknown project: status=%d cache=%q", response.Code, response.Header().Get("Cache-Control"))
	}
	assertSecurityHeaders(t, "unknown project", response)

	asset := serve(h, httptest.NewRequest(http.MethodGet, "/assets/"+assetHash+"/app.js", nil))
	if asset.Code != http.StatusOK || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset: status=%d cache=%q", asset.Code, asset.Header().Get("Cache-Control"))
	}
}

func TestEventStreamHeaders(t *testing.T) {
	h := newTestHandler(t, time.Now())
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	cookie := sessionCookie(t, h, loginRequest(string(webToken)))
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/events", nil)
	request.AddCookie(cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buffer := make([]byte, 64)
	n, _ := response.Body.Read(buffer)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" ||
		response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Content-Security-Policy") == "" ||
		!strings.HasPrefix(string(buffer[:n]), "event: state\ndata: {\"version\":\"test\"") {
		t.Fatalf("events: status=%d headers=%v first=%q", response.StatusCode, response.Header, buffer[:n])
	}
	// Shutdown ends open streams instead of holding the server's shutdown.
	h.Shutdown()
	done := make(chan struct{})
	go func() {
		for {
			if _, err := response.Body.Read(buffer); err != nil {
				close(done)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream outlived Shutdown")
	}
}

func TestRenderANSI(t *testing.T) {
	for _, test := range []struct{ name, in, want string }{
		{"escapes HTML", "<b>&", "&lt;b&gt;&amp;"},
		{"maps SGR to classes", "\x1b[1;31mred\x1b[0m plain", `<span class="b f1">red</span> plain`},
		{"keeps bright and 256 palette colors", "\x1b[92mA\x1b[38;5;4mB\x1b[38;2;1;2;3mC", `<span class="f10">A</span><span class="f4">B</span>C`},
		{"drops OSC 8 hyperlinks", "\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\", "click"},
		{"drops title OSC", "\x1b]0;pwned\x07after", "after"},
		{"drops C1 controls", "a\u009b2Kb\u0085c\u009d0;t\u009cd", "abcd"},
		{"drops C0 but keeps tabs", "a\rb\x08c\td\x00", "abc\td"},
		{"drops other CSI", "\x1b[2Ka\x1b[1Ab", "ab"},
	} {
		if got := string(renderANSI([]byte(test.in))); got != test.want {
			t.Errorf("%s: renderANSI(%q) = %q, want %q", test.name, test.in, got, test.want)
		}
	}
}
