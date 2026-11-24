// Package web serves the optional read-only web viewer on the scheduler
// listener. Reads go through internal/app; this package only adapts them.
package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brettinternet/noon/internal/config"
)

const (
	cookieName     = "noon_session"
	sessionTTL     = 30 * 24 * time.Hour
	maxStreams     = 32
	maxLoginBytes  = 4 << 10
	pageWriteLimit = 30 * time.Second
)

// Options configures a viewer. Token is the web token; it signs sessions and
// is never written anywhere.
type Options struct {
	Token    []byte
	Input    config.Input
	StateDir string
	Version  string
	Logf     func(format string, args ...any)
	Now      func() time.Time
}

// Handler serves every web route. Mount it only when a web token is configured.
type Handler struct {
	options     Options
	tokenDigest [sha256.Size]byte
	mux         *http.ServeMux
	watcher     *watcher
	streams     atomic.Int32
	done        chan struct{}
	closeOnce   sync.Once
}

// New builds a viewer and starts its single state watcher.
func New(options Options) *Handler {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logf == nil {
		options.Logf = func(string, ...any) {}
	}
	h := &Handler{options: options, tokenDigest: sha256.Sum256(options.Token), done: make(chan struct{})}
	h.watcher = newWatcher(options, h.done)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("GET /{$}", h.page(h.overview))
	mux.HandleFunc("GET /projects/{id}", h.page(h.project))
	mux.HandleFunc("GET /runs/{id}", h.page(h.run))
	mux.HandleFunc("GET /events", h.events)
	mux.HandleFunc("GET /assets/{hash}/{name}", serveAsset)
	h.mux = mux
	return h
}

// Shutdown ends every open stream and the watcher. Register it with
// http.Server.RegisterOnShutdown so tabs never hold the shutdown window.
func (h *Handler) Shutdown() {
	h.closeOnce.Do(func() { close(h.done) })
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "same-origin")
	// The zero value trusts no other origin and bypasses nothing.
	var protection http.CrossOriginProtection
	protection.Handler(h.mux).ServeHTTP(w, r)
}

// page requires a session and renders without caching, because logs can hold secrets.
func (h *Handler) page(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.signedIn(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(pageWriteLimit))
		next(w, r)
	}
}

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if h.signedIn(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.render(w, r, http.StatusOK, "login", h.layout(r, "Sign in", nil))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBytes)
	submitted := sha256.Sum256([]byte(r.PostFormValue("token")))
	if subtle.ConstantTimeCompare(submitted[:], h.tokenDigest[:]) != 1 {
		h.options.Logf("web sign-in failed remote=%s", r.RemoteAddr)
		h.render(w, r, http.StatusUnauthorized, "login", h.layout(r, "Sign in", map[string]any{"Failed": true}))
		return
	}
	expires := h.options.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: h.sign(expires), Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureRequest(r),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureRequest(r)})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// secureRequest reports HTTPS directly or through a proxy that says so.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sign returns "EXPIRY.MAC", where MAC is an HMAC-SHA256 of the expiry keyed
// by the web token, so changing the token signs everyone out.
func (h *Handler) sign(expires time.Time) string {
	unix := strconv.FormatInt(expires.Unix(), 10)
	return unix + "." + base64.RawURLEncoding.EncodeToString(h.mac(unix))
}

func (h *Handler) mac(expiry string) []byte {
	mac := hmac.New(sha256.New, h.options.Token)
	mac.Write([]byte(cookieName + ":" + expiry))
	return mac.Sum(nil)
}

func (h *Handler) signedIn(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	expiry, encoded, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return false
	}
	unix, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || !h.options.Now().Before(time.Unix(unix, 0)) {
		return false
	}
	actual, err := base64.RawURLEncoding.DecodeString(encoded)
	return err == nil && hmac.Equal(actual, h.mac(expiry))
}
