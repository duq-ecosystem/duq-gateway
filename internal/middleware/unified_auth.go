package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"duq-gateway/internal/db"
)

// UnifiedAuth middleware handles authentication for both mobile and telegram channels
// Currently supports mobile session tokens (mob_* prefix)
// Future: can be extended for telegram web sessions
func UnifiedAuth(dbClient *db.Client, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// Authorization header didn't have "Bearer " prefix
			http.Error(w, "Unauthorized: invalid Authorization format", http.StatusUnauthorized)
			return
		}

		// Check for mobile session token
		if strings.HasPrefix(token, "mob_") {
			session, err := dbClient.GetMobileSession(token)
			if err != nil || session == nil {
				http.Error(w, "Unauthorized: invalid mobile token", http.StatusUnauthorized)
				return
			}

			// Check if token is expired
			if time.Now().After(session.ExpiresAt) {
				http.Error(w, "Unauthorized: token expired", http.StatusUnauthorized)
				return
			}

			// Update last activity (async, don't block request)
			go dbClient.UpdateSessionActivity(token)

			// Look up user preferences by telegram_id
			var userID int64
			var role string
			var timezone string
			var preferredLanguage string
			query := `SELECT id, COALESCE(role, 'user'),
			          COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
			          FROM users WHERE telegram_id = $1`
			err = dbClient.DB().QueryRow(query, session.TelegramID).Scan(
				&userID, &role, &timezone, &preferredLanguage)
			if err != nil {
				// User not found in users table - use defaults
				userID = 0
				role = "user"
				timezone = "UTC"
				preferredLanguage = "ru"
			}

			// Add user info and preferences to request context
			ctx := context.WithValue(r.Context(), "telegram_id", session.TelegramID)
			ctx = context.WithValue(ctx, "user_id", userID)
			ctx = context.WithValue(ctx, "role", role)
			ctx = context.WithValue(ctx, "timezone", timezone)
			ctx = context.WithValue(ctx, "preferred_language", preferredLanguage)
			ctx = context.WithValue(ctx, "channel", "mobile")
			next(w, r.WithContext(ctx))
			return
		}

		// Future: Add support for telegram web sessions or other auth methods here
		// if strings.HasPrefix(token, "tg_") { ... }

		http.Error(w, "Unauthorized: unknown token type", http.StatusUnauthorized)
	}
}
