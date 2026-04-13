package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

type Claims struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JWTAuth middleware validates JWT tokens and adds user_id and role to request context
func JWTAuth(cfg *config.Config, dbClient *db.Client, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Unauthorized: invalid Authorization format", http.StatusUnauthorized)
			return
		}

		// Parse and validate token
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(cfg.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		// Verify user is active in database and get preferences
		var isActive bool
		var telegramID int64
		var timezone string
		var preferredLanguage string
		query := `SELECT is_active, COALESCE(telegram_id, 0),
		          COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
		          FROM users WHERE id = $1`
		err = dbClient.DB().QueryRow(query, claims.UserID).Scan(
			&isActive, &telegramID, &timezone, &preferredLanguage)
		if err != nil {
			http.Error(w, "Unauthorized: user not found", http.StatusUnauthorized)
			return
		}

		if !isActive {
			http.Error(w, "Forbidden: account disabled", http.StatusForbidden)
			return
		}

		// Add user info and preferences to request context
		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		ctx = context.WithValue(ctx, "telegram_id", telegramID)
		ctx = context.WithValue(ctx, "role", claims.Role)
		ctx = context.WithValue(ctx, "timezone", timezone)
		ctx = context.WithValue(ctx, "preferred_language", preferredLanguage)
		next(w, r.WithContext(ctx))
	}
}
