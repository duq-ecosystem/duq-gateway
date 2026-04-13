package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// keycloakHTTPClient is a shared HTTP client for Keycloak requests
// Initialized via InitKeycloakClient with configured timeout
var keycloakHTTPClient *http.Client

// InitKeycloakClient initializes the Keycloak HTTP client with configured timeout
func InitKeycloakClient(cfg *config.Config) {
	timeout := time.Duration(cfg.Timeouts.KeycloakTimeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second // fallback default
	}
	keycloakHTTPClient = &http.Client{
		Timeout: timeout,
	}
}

// getKeycloakClient returns the shared Keycloak HTTP client
// Falls back to default if not initialized
func getKeycloakClient() *http.Client {
	if keycloakHTTPClient != nil {
		return keycloakHTTPClient
	}
	// Fallback if not initialized (shouldn't happen in production)
	return &http.Client{Timeout: 10 * time.Second}
}

// KeycloakTokenResponse represents the token response from Keycloak
type KeycloakTokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	IDToken          string `json:"id_token,omitempty"`
	NotBeforePolicy  int    `json:"not-before-policy"`
	SessionState     string `json:"session_state"`
	Scope            string `json:"scope"`
}

// KeycloakErrorResponse represents an error from Keycloak
type KeycloakErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// KeycloakLogin redirects user to Keycloak login page
func KeycloakLogin(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Keycloak.Enabled {
			http.Error(w, "Keycloak authentication is not enabled", http.StatusNotImplemented)
			return
		}

		// Build authorization URL
		authURL := fmt.Sprintf(
			"%s/realms/%s/protocol/openid-connect/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+profile+email",
			cfg.Keycloak.URL,
			cfg.Keycloak.Realm,
			url.QueryEscape(cfg.Keycloak.ClientID),
			url.QueryEscape(getRedirectURI(cfg)),
		)

		log.Printf("[keycloak] Redirecting to login: %s", authURL)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// KeycloakCallback handles the OAuth2 callback from Keycloak
func KeycloakCallback(cfg *config.Config, dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Keycloak.Enabled {
			http.Error(w, "Keycloak authentication is not enabled", http.StatusNotImplemented)
			return
		}

		// Check for error from Keycloak
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errDesc := r.URL.Query().Get("error_description")
			log.Printf("[keycloak] Auth error: %s - %s", errMsg, errDesc)
			http.Error(w, fmt.Sprintf("Authentication error: %s", errDesc), http.StatusBadRequest)
			return
		}

		// Get authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		// Exchange code for tokens
		tokenResp, err := exchangeCodeForTokens(cfg, code)
		if err != nil {
			log.Printf("[keycloak] Token exchange failed: %v", err)
			http.Error(w, "Failed to exchange code for tokens", http.StatusInternalServerError)
			return
		}

		log.Printf("[keycloak] Token exchange successful, access_token expires in %d seconds", tokenResp.ExpiresIn)

		// Return tokens to client
		// In a browser flow, you might want to set cookies or redirect to frontend
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  tokenResp.AccessToken,
			"refresh_token": tokenResp.RefreshToken,
			"expires_in":    tokenResp.ExpiresIn,
			"token_type":    tokenResp.TokenType,
		})
	}
}

// KeycloakRefresh handles token refresh
func KeycloakRefresh(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Keycloak.Enabled {
			http.Error(w, "Keycloak authentication is not enabled", http.StatusNotImplemented)
			return
		}

		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.RefreshToken == "" {
			http.Error(w, "Missing refresh_token", http.StatusBadRequest)
			return
		}

		// Refresh tokens
		tokenResp, err := refreshTokens(cfg, req.RefreshToken)
		if err != nil {
			log.Printf("[keycloak] Token refresh failed: %v", err)
			http.Error(w, "Failed to refresh tokens", http.StatusUnauthorized)
			return
		}

		log.Printf("[keycloak] Token refresh successful")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  tokenResp.AccessToken,
			"refresh_token": tokenResp.RefreshToken,
			"expires_in":    tokenResp.ExpiresIn,
			"token_type":    tokenResp.TokenType,
		})
	}
}

// KeycloakLogout handles user logout
func KeycloakLogout(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Keycloak.Enabled {
			http.Error(w, "Keycloak authentication is not enabled", http.StatusNotImplemented)
			return
		}

		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// If no body, just redirect to Keycloak logout
			logoutURL := fmt.Sprintf(
				"%s/realms/%s/protocol/openid-connect/logout?redirect_uri=%s",
				cfg.Keycloak.URL,
				cfg.Keycloak.Realm,
				url.QueryEscape(getRedirectURI(cfg)),
			)
			http.Redirect(w, r, logoutURL, http.StatusFound)
			return
		}

		// If refresh token provided, revoke it
		if req.RefreshToken != "" {
			if err := revokeToken(cfg, req.RefreshToken); err != nil {
				log.Printf("[keycloak] Token revocation failed: %v", err)
				// Continue anyway
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "logged_out",
		})
	}
}

// KeycloakUserInfo returns user info from token
func KeycloakUserInfo(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Keycloak.Enabled {
			http.Error(w, "Keycloak authentication is not enabled", http.StatusNotImplemented)
			return
		}

		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" || token == authHeader {
			http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}

		// Call Keycloak userinfo endpoint
		userInfoURL := fmt.Sprintf(
			"%s/realms/%s/protocol/openid-connect/userinfo",
			cfg.Keycloak.URL,
			cfg.Keycloak.Realm,
		)

		req, _ := http.NewRequest("GET", userInfoURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		client := getKeycloakClient()
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[keycloak] Userinfo request failed: %v", err)
			http.Error(w, "Failed to get user info", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("[keycloak] Userinfo returned %d: %s", resp.StatusCode, string(body))
			http.Error(w, "Failed to get user info", resp.StatusCode)
			return
		}

		// Forward response
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, resp.Body)
	}
}

// exchangeCodeForTokens exchanges authorization code for tokens
func exchangeCodeForTokens(cfg *config.Config, code string) (*KeycloakTokenResponse, error) {
	tokenURL := fmt.Sprintf(
		"%s/realms/%s/protocol/openid-connect/token",
		cfg.Keycloak.URL,
		cfg.Keycloak.Realm,
	)

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", cfg.Keycloak.ClientID)
	data.Set("client_secret", cfg.Keycloak.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", getRedirectURI(cfg))

	return doTokenRequest(tokenURL, data)
}

// refreshTokens refreshes access token using refresh token
func refreshTokens(cfg *config.Config, refreshToken string) (*KeycloakTokenResponse, error) {
	tokenURL := fmt.Sprintf(
		"%s/realms/%s/protocol/openid-connect/token",
		cfg.Keycloak.URL,
		cfg.Keycloak.Realm,
	)

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", cfg.Keycloak.ClientID)
	data.Set("client_secret", cfg.Keycloak.ClientSecret)
	data.Set("refresh_token", refreshToken)

	return doTokenRequest(tokenURL, data)
}

// revokeToken revokes a refresh token
func revokeToken(cfg *config.Config, refreshToken string) error {
	revokeURL := fmt.Sprintf(
		"%s/realms/%s/protocol/openid-connect/revoke",
		cfg.Keycloak.URL,
		cfg.Keycloak.Realm,
	)

	data := url.Values{}
	data.Set("client_id", cfg.Keycloak.ClientID)
	data.Set("client_secret", cfg.Keycloak.ClientSecret)
	data.Set("token", refreshToken)
	data.Set("token_type_hint", "refresh_token")

	client := getKeycloakClient()
	resp, err := client.PostForm(revokeURL, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// doTokenRequest performs a token request to Keycloak
func doTokenRequest(tokenURL string, data url.Values) (*KeycloakTokenResponse, error) {
	client := getKeycloakClient()
	resp, err := client.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp KeycloakErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil {
			return nil, fmt.Errorf("%s: %s", errResp.Error, errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp KeycloakTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &tokenResp, nil
}

// getRedirectURI returns the OAuth2 redirect URI
func getRedirectURI(cfg *config.Config) string {
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/auth/keycloak/callback", scheme, cfg.GatewayHost)
}
