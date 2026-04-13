package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CSRFConfig holds configuration for CSRF protection middleware.
type CSRFConfig struct {
	// TokenLength is the length of the random token in bytes (default: 32)
	TokenLength int

	// TokenLookup defines where to find the token: "header:X-CSRF-Token" or "form:csrf_token"
	TokenLookup string

	// CookieName is the name of the cookie storing the CSRF token
	CookieName string

	// CookiePath is the path for the CSRF cookie
	CookiePath string

	// CookieMaxAge is the max age of the CSRF cookie in seconds (default: 86400 = 24h)
	CookieMaxAge int

	// CookieSecure sets the Secure flag on the cookie
	CookieSecure bool

	// CookieSameSite sets the SameSite attribute (Strict, Lax, None)
	CookieSameSite http.SameSite

	// ExcludePaths are paths that don't require CSRF validation (e.g., webhooks)
	ExcludePaths []string

	// ExcludePrefixes are path prefixes that don't require CSRF validation
	ExcludePrefixes []string

	// ErrorHandler is called when CSRF validation fails
	ErrorHandler func(w http.ResponseWriter, r *http.Request)
}

// DefaultCSRFConfig returns a default CSRF configuration.
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		TokenLength:    32,
		TokenLookup:    "header:X-CSRF-Token",
		CookieName:     "_csrf",
		CookiePath:     "/",
		CookieMaxAge:   86400, // 24 hours
		CookieSecure:   true,
		CookieSameSite: http.SameSiteStrictMode,
		ExcludePaths: []string{
			"/health",
			"/api/telegram/webhook",
			"/api/github",
			"/api/calendar",
			"/api/gmail",
			"/api/custom",
			"/api/duq/callback",
		},
		ExcludePrefixes: []string{
			"/api/auth/keycloak/",      // OAuth callbacks
			"/api/auth/google/",        // OAuth callbacks
			"/api/oauth/",              // OAuth flows
		},
		ErrorHandler: defaultCSRFErrorHandler,
	}
}

func defaultCSRFErrorHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "CSRF token invalid or missing", http.StatusForbidden)
}

// CSRFToken represents a CSRF token with creation time for expiry.
type CSRFToken struct {
	Token     string
	CreatedAt time.Time
}

// CSRFStore stores active CSRF tokens with thread-safe access.
type CSRFStore struct {
	mu     sync.RWMutex
	tokens map[string]CSRFToken // token -> CSRFToken
	ttl    time.Duration
}

// NewCSRFStore creates a new CSRF token store with cleanup.
func NewCSRFStore(ttl time.Duration) *CSRFStore {
	store := &CSRFStore{
		tokens: make(map[string]CSRFToken),
		ttl:    ttl,
	}

	// Start cleanup goroutine
	go store.cleanup()

	return store
}

// Generate creates a new CSRF token.
func (s *CSRFStore) Generate(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	token := base64.URLEncoding.EncodeToString(b)

	s.mu.Lock()
	s.tokens[token] = CSRFToken{
		Token:     token,
		CreatedAt: time.Now(),
	}
	s.mu.Unlock()

	return token, nil
}

// Validate checks if a token is valid.
func (s *CSRFStore) Validate(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stored, exists := s.tokens[token]
	if !exists {
		return false
	}

	// Check if token has expired
	if time.Since(stored.CreatedAt) > s.ttl {
		return false
	}

	return true
}

// cleanup removes expired tokens periodically.
func (s *CSRFStore) cleanup() {
	ticker := time.NewTicker(s.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, data := range s.tokens {
			if now.Sub(data.CreatedAt) > s.ttl {
				delete(s.tokens, token)
			}
		}
		s.mu.Unlock()
	}
}

// CSRF returns a middleware that protects against CSRF attacks.
// It generates tokens for GET requests and validates them on POST/PUT/DELETE.
//
// For APIs using Bearer tokens, CSRF protection is not needed as Bearer tokens
// are not automatically sent by browsers. This middleware is primarily for
// form-based submissions and cookie-authenticated endpoints.
func CSRF(cfg CSRFConfig, store *CSRFStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Check if path is excluded
			if isCSRFExcluded(path, cfg.ExcludePaths, cfg.ExcludePrefixes) {
				next.ServeHTTP(w, r)
				return
			}

			// Skip CSRF for requests with Bearer token (API calls)
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			// For safe methods (GET, HEAD, OPTIONS), generate token if needed
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				// Check if token already exists in cookie
				cookie, err := r.Cookie(cfg.CookieName)
				var token string

				if err != nil || cookie.Value == "" || !store.Validate(cookie.Value) {
					// Generate new token
					token, err = store.Generate(cfg.TokenLength)
					if err != nil {
						http.Error(w, "Failed to generate CSRF token", http.StatusInternalServerError)
						return
					}

					// Set cookie with new token
					http.SetCookie(w, &http.Cookie{
						Name:     cfg.CookieName,
						Value:    token,
						Path:     cfg.CookiePath,
						MaxAge:   cfg.CookieMaxAge,
						HttpOnly: false, // JavaScript needs to read it
						Secure:   cfg.CookieSecure,
						SameSite: cfg.CookieSameSite,
					})
				} else {
					token = cookie.Value
				}

				// Add token to response header for easy access
				w.Header().Set("X-CSRF-Token", token)

				next.ServeHTTP(w, r)
				return
			}

			// For unsafe methods (POST, PUT, DELETE, PATCH), validate token
			if r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodDelete || r.Method == http.MethodPatch {

				// Get token from request
				var requestToken string

				// Parse token lookup
				parts := strings.SplitN(cfg.TokenLookup, ":", 2)
				if len(parts) != 2 {
					cfg.ErrorHandler(w, r)
					return
				}

				switch parts[0] {
				case "header":
					requestToken = r.Header.Get(parts[1])
				case "form":
					requestToken = r.FormValue(parts[1])
				case "query":
					requestToken = r.URL.Query().Get(parts[1])
				}

				// Also check cookie token
				cookie, err := r.Cookie(cfg.CookieName)
				if err != nil {
					cfg.ErrorHandler(w, r)
					return
				}

				// Validate: request token must match cookie token AND be in store
				if requestToken == "" ||
					subtle.ConstantTimeCompare([]byte(requestToken), []byte(cookie.Value)) != 1 ||
					!store.Validate(cookie.Value) {
					cfg.ErrorHandler(w, r)
					return
				}

				next.ServeHTTP(w, r)
				return
			}

			// For other methods, just pass through
			next.ServeHTTP(w, r)
		})
	}
}

// isCSRFExcluded checks if a path should be excluded from CSRF protection.
func isCSRFExcluded(path string, excludePaths, excludePrefixes []string) bool {
	// Check exact paths
	for _, p := range excludePaths {
		if path == p {
			return true
		}
	}

	// Check prefixes
	for _, prefix := range excludePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}
