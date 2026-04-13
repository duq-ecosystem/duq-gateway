package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// KeycloakClaims represents the JWT claims from Keycloak
type KeycloakClaims struct {
	Sub               string                 `json:"sub"`
	PreferredUsername string                 `json:"preferred_username"`
	Email             string                 `json:"email"`
	EmailVerified     bool                   `json:"email_verified"`
	Name              string                 `json:"name"`
	GivenName         string                 `json:"given_name"`
	FamilyName        string                 `json:"family_name"`
	RealmAccess       RealmAccess            `json:"realm_access"`
	ResourceAccess    map[string]RealmAccess `json:"resource_access"`
	jwt.RegisteredClaims
}

// RealmAccess represents Keycloak realm access roles
type RealmAccess struct {
	Roles []string `json:"roles"`
}

// LocalUserInfo holds local user data from Gateway database
type LocalUserInfo struct {
	UserID            int64
	TelegramID        int64
	Role              string
	Timezone          string
	PreferredLanguage string
}

// JWKSCache caches the JWKS keys from Keycloak
type JWKSCache struct {
	mu              sync.RWMutex
	keys            map[string]interface{}
	lastFetch       time.Time
	cacheTTL        time.Duration
	keycloakTimeout time.Duration // HTTP timeout for Keycloak requests
	keycloakURL     string
	realm           string
}

// JWKS represents the JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

var (
	jwksCache     *JWKSCache
	jwksCacheOnce sync.Once
)

// getJWKSCache returns the singleton JWKS cache
// jwksTTLMin: JWKS cache TTL in minutes (default 10 if 0)
// keycloakTimeoutSec: HTTP timeout for Keycloak in seconds (default 10 if 0)
func getJWKSCache(keycloakURL, realm string, jwksTTLMin int, keycloakTimeoutSec int) *JWKSCache {
	jwksCacheOnce.Do(func() {
		ttl := time.Duration(jwksTTLMin) * time.Minute
		if ttl == 0 {
			ttl = 10 * time.Minute // fallback default
		}
		timeout := time.Duration(keycloakTimeoutSec) * time.Second
		if timeout == 0 {
			timeout = 10 * time.Second // fallback default
		}
		jwksCache = &JWKSCache{
			keys:            make(map[string]interface{}),
			cacheTTL:        ttl,
			keycloakTimeout: timeout,
			keycloakURL:     keycloakURL,
			realm:           realm,
		}
	})
	return jwksCache
}

// fetchJWKS fetches JWKS from Keycloak
func (c *JWKSCache) fetchJWKS() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if cache is still valid
	if time.Since(c.lastFetch) < c.cacheTTL && len(c.keys) > 0 {
		return nil
	}

	url := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", c.keycloakURL, c.realm)

	client := &http.Client{Timeout: c.keycloakTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("failed to decode JWKS: %w", err)
	}

	// Parse and cache keys
	c.keys = make(map[string]interface{})
	for _, key := range jwks.Keys {
		if key.Kty == "RSA" && key.Use == "sig" {
			pubKey, err := parseRSAPublicKey(key.N, key.E)
			if err != nil {
				log.Printf("[keycloak] Failed to parse RSA key %s: %v", key.Kid, err)
				continue
			}
			c.keys[key.Kid] = pubKey
		}
	}

	c.lastFetch = time.Now()
	log.Printf("[keycloak] JWKS cache refreshed, %d keys loaded", len(c.keys))
	return nil
}

// getKey returns the public key for a given key ID
func (c *JWKSCache) getKey(kid string) (interface{}, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	c.mu.RUnlock()

	if !ok {
		// Try to refresh cache
		if err := c.fetchJWKS(); err != nil {
			return nil, err
		}
		c.mu.RLock()
		key, ok = c.keys[kid]
		c.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("key %s not found in JWKS", kid)
		}
	}

	return key, nil
}

// parseRSAPublicKey parses RSA public key from JWK n and e values
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	// Decode n (modulus)
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode n: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode e (exponent)
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode e: %w", err)
	}

	// Convert e bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

// KeycloakAuth middleware validates Keycloak JWT tokens
func KeycloakAuth(cfg *config.Config, dbClient *db.Client, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Keycloak.Enabled {
			// Keycloak not enabled, skip
			next(w, r)
			return
		}

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

		// Skip if it's a mobile token (handled by MobileAuth)
		if strings.HasPrefix(tokenString, "mob_") {
			next(w, r)
			return
		}

		// Get JWKS cache
		cache := getJWKSCache(cfg.Keycloak.URL, cfg.Keycloak.Realm, cfg.Timeouts.JWKSCacheTTLMin, cfg.Timeouts.KeycloakTimeout)

		// Ensure JWKS is fetched
		if err := cache.fetchJWKS(); err != nil {
			log.Printf("[keycloak] Failed to fetch JWKS: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Parse and validate token
		claims := &KeycloakClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			// Verify signing method
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			// Get key ID from token header
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, fmt.Errorf("missing kid in token header")
			}

			return cache.getKey(kid)
		})

		if err != nil {
			log.Printf("[keycloak] Token validation failed: %v", err)
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		if !token.Valid {
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		// Verify issuer matches our Keycloak realm
		expectedIssuer := fmt.Sprintf("%s/realms/%s", cfg.Keycloak.URL, cfg.Keycloak.Realm)
		if claims.Issuer != expectedIssuer {
			log.Printf("[keycloak] Invalid issuer: expected %s, got %s", expectedIssuer, claims.Issuer)
			http.Error(w, "Unauthorized: invalid token issuer", http.StatusUnauthorized)
			return
		}

		// Map Keycloak user to local user
		userInfo, err := mapKeycloakUser(dbClient, claims)
		if err != nil {
			log.Printf("[keycloak] Failed to map user: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Add user info and preferences to request context
		ctx := context.WithValue(r.Context(), "user_id", userInfo.UserID)
		ctx = context.WithValue(ctx, "telegram_id", userInfo.TelegramID)
		ctx = context.WithValue(ctx, "role", userInfo.Role)
		ctx = context.WithValue(ctx, "timezone", userInfo.Timezone)
		ctx = context.WithValue(ctx, "preferred_language", userInfo.PreferredLanguage)
		ctx = context.WithValue(ctx, "keycloak_sub", claims.Sub)
		ctx = context.WithValue(ctx, "keycloak_email", claims.Email)
		ctx = context.WithValue(ctx, "keycloak_username", claims.PreferredUsername)

		log.Printf("[keycloak] Authenticated user: sub=%s, username=%s, user_id=%d, telegram_id=%d, role=%s, tz=%s, lang=%s",
			claims.Sub, claims.PreferredUsername, userInfo.UserID, userInfo.TelegramID, userInfo.Role,
			userInfo.Timezone, userInfo.PreferredLanguage)

		next(w, r.WithContext(ctx))
	}
}

// mapKeycloakUser maps a Keycloak user to a local user, creating one if necessary
// Returns LocalUserInfo with all user data including preferences
func mapKeycloakUser(dbClient *db.Client, claims *KeycloakClaims) (*LocalUserInfo, error) {
	// Determine role from Keycloak realm roles
	keycloakRole := "user"
	for _, r := range claims.RealmAccess.Roles {
		if r == "admin" {
			keycloakRole = "admin"
			break
		}
	}

	// Try to find existing user by keycloak_sub
	var userID int64
	var telegramID int64
	var role string
	var isActive bool
	var timezone string
	var preferredLanguage string

	query := `SELECT id, COALESCE(telegram_id, 0), role, is_active,
	          COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
	          FROM users WHERE keycloak_sub = $1`
	err := dbClient.DB().QueryRow(query, claims.Sub).Scan(
		&userID, &telegramID, &role, &isActive, &timezone, &preferredLanguage)

	if err == nil {
		// User found
		if !isActive {
			return nil, fmt.Errorf("user account disabled")
		}

		// Sync role from Keycloak if changed
		if role != keycloakRole {
			updateQuery := `UPDATE users SET role = $1 WHERE id = $2`
			_, updateErr := dbClient.DB().Exec(updateQuery, keycloakRole, userID)
			if updateErr != nil {
				log.Printf("[keycloak] Failed to sync role for user %d: %v", userID, updateErr)
			} else {
				log.Printf("[keycloak] Synced role for user %d: %s -> %s", userID, role, keycloakRole)
				role = keycloakRole
			}
		}

		return &LocalUserInfo{
			UserID:            userID,
			TelegramID:        telegramID,
			Role:              role,
			Timezone:          timezone,
			PreferredLanguage: preferredLanguage,
		}, nil
	}

	// Try to find by email
	query = `SELECT id, COALESCE(telegram_id, 0), role, is_active,
	         COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
	         FROM users WHERE email = $1`
	err = dbClient.DB().QueryRow(query, claims.Email).Scan(
		&userID, &telegramID, &role, &isActive, &timezone, &preferredLanguage)

	if err == nil {
		// User found by email, update keycloak_sub and sync role
		if !isActive {
			return nil, fmt.Errorf("user account disabled")
		}

		updateQuery := `UPDATE users SET keycloak_sub = $1, role = $2 WHERE id = $3`
		_, err = dbClient.DB().Exec(updateQuery, claims.Sub, keycloakRole, userID)
		if err != nil {
			log.Printf("[keycloak] Failed to update keycloak_sub for user %d: %v", userID, err)
		} else if role != keycloakRole {
			log.Printf("[keycloak] Synced role for user %d: %s -> %s", userID, role, keycloakRole)
		}

		return &LocalUserInfo{
			UserID:            userID,
			TelegramID:        telegramID,
			Role:              keycloakRole,
			Timezone:          timezone,
			PreferredLanguage: preferredLanguage,
		}, nil
	}

	// User not found, create new user (no telegram_id for new Keycloak users)
	// Use ON CONFLICT to handle race condition when multiple requests arrive
	// for the same new user simultaneously
	role = keycloakRole
	telegramID = 0
	timezone = "UTC"
	preferredLanguage = "ru"

	insertQuery := `
		INSERT INTO users (username, email, keycloak_sub, role, is_active, created_at, timezone, preferred_language)
		VALUES ($1, $2, $3, $4, true, NOW(), 'UTC', 'ru')
		ON CONFLICT (keycloak_sub) DO UPDATE SET
			username = EXCLUDED.username,
			email = EXCLUDED.email,
			role = EXCLUDED.role
		RETURNING id, COALESCE(telegram_id, 0), COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
	`
	err = dbClient.DB().QueryRow(
		insertQuery,
		claims.PreferredUsername,
		claims.Email,
		claims.Sub,
		role,
	).Scan(&userID, &telegramID, &timezone, &preferredLanguage)

	if err != nil {
		return nil, fmt.Errorf("failed to create/upsert user: %w", err)
	}

	log.Printf("[keycloak] User upserted: id=%d, username=%s, email=%s, role=%s, telegram_id=%d, tz=%s, lang=%s",
		userID, claims.PreferredUsername, claims.Email, role, telegramID, timezone, preferredLanguage)

	return &LocalUserInfo{
		UserID:            userID,
		TelegramID:        telegramID,
		Role:              role,
		Timezone:          timezone,
		PreferredLanguage: preferredLanguage,
	}, nil
}

// KeycloakOrJWT middleware tries Keycloak first, then falls back to legacy JWT
func KeycloakOrJWT(cfg *config.Config, dbClient *db.Client, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// If Keycloak is enabled and token is not a mobile token, use Keycloak auth
		if cfg.Keycloak.Enabled && !strings.HasPrefix(tokenString, "mob_") {
			// Try to validate as Keycloak token
			KeycloakAuth(cfg, dbClient, next)(w, r)
			return
		}

		// Fall back to legacy JWT auth
		JWTAuth(cfg, dbClient, next)(w, r)
	}
}
