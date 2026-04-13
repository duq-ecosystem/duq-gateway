package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// BasicAuth middleware for protecting web pages with login/password
func BasicAuth(cfg *config.Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Credentials must be configured
		if cfg.BasicAuth.Username == "" || cfg.BasicAuth.Password == "" {
			http.Error(w, "BasicAuth not configured", http.StatusInternalServerError)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Duq Gateway"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(cfg.BasicAuth.Username)) == 1
		passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(cfg.BasicAuth.Password)) == 1

		if !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="Duq Gateway"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// MobileAuth middleware for mobile app token authentication
func MobileAuth(dbClient *db.Client, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")

		if token == "" || !strings.HasPrefix(token, "mob_") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Lookup session in database
		session, err := dbClient.GetMobileSession(token)
		if err != nil || session == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Check if token is expired
		if time.Now().After(session.ExpiresAt) {
			http.Error(w, "Token expired", http.StatusUnauthorized)
			return
		}

		// Update last activity (async, don't block request)
		go dbClient.UpdateSessionActivity(token)

		// Add telegram_id to request context
		ctx := context.WithValue(r.Context(), "telegram_id", session.TelegramID)
		next(w, r.WithContext(ctx))
	}
}
