package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// Allow the existing 10s body-read timeout, a full 60s get_run wait,
// and time to encode and write the response.
const apiWriteTimeout = 75 * time.Second

// NewListenerHandler mounts the existing webhook/health handler and, when a
// token is configured, the authenticated JSON API and stateless MCP transport.
func NewListenerHandler(webhooks, api, mcp http.Handler, token []byte) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", webhooks)
	mux.Handle("/hooks/", webhooks)
	if len(token) > 0 {
		mux.Handle("/v1/", extendWriteDeadline(api))
		mux.Handle("/mcp", extendWriteDeadline(mcp))
	}
	mux.Handle("/", webhooks)
	if len(token) == 0 {
		return mux
	}
	digest := sha256.Sum256(token)
	return authenticate(digest, mux)
}

func authenticate(tokenDigest [sha256.Size]byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requiresBearer(r.URL.Path) && !hasBearerToken(r, tokenDigest) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requiresBearer(path string) bool {
	return path == "/v1" || strings.HasPrefix(path, "/v1/") || path == "/mcp" || strings.HasPrefix(path, "/mcp/")
}

func hasBearerToken(r *http.Request, expected [sha256.Size]byte) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || scheme != "Bearer" || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return false
	}
	actual := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}

func extendWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(apiWriteTimeout))
		next.ServeHTTP(w, r)
	})
}
