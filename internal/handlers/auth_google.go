package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/credentials"
)

// OAuthState holds pending OAuth states
type OAuthState struct {
	UserID    int64
	ChatID    int64
	CreatedAt time.Time
}

var (
	oauthStates   = make(map[string]*OAuthState)
	oauthStatesMu sync.RWMutex
)

// Google OAuth scopes
var googleScopes = []string{
	"https://www.googleapis.com/auth/userinfo.email", // For getting user's email
	"https://www.googleapis.com/auth/calendar",
	"https://www.googleapis.com/auth/calendar.events",
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/tasks",
	"https://www.googleapis.com/auth/tasks.readonly",
	"https://www.googleapis.com/auth/drive.readonly",
}

// GoogleOAuthDeps holds dependencies for Google OAuth handlers
type GoogleOAuthDeps struct {
	Config      *config.Config
	CredService *credentials.Service
	SendMessage func(chatID int64, text string) error
}

// GenerateOAuthURL generates a Google OAuth URL for a user
func GenerateOAuthURL(cfg *config.Config, userID, chatID int64) (string, error) {
	// Generate random state
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	state := base64.URLEncoding.EncodeToString(stateBytes)

	// Store state
	oauthStatesMu.Lock()
	oauthStates[state] = &OAuthState{
		UserID:    userID,
		ChatID:    chatID,
		CreatedAt: time.Now(),
	}
	oauthStatesMu.Unlock()

	// Build OAuth URL
	params := url.Values{
		"client_id":     {cfg.GoogleOAuth.ClientID},
		"redirect_uri":  {cfg.GoogleOAuth.RedirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(googleScopes, " ")},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}

	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode(), nil
}

// GoogleOAuthCallback handles the OAuth callback from Google
func GoogleOAuthCallback(deps *GoogleOAuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errorParam := r.URL.Query().Get("error")

		if errorParam != "" {
			log.Printf("[google-oauth] Error from Google: %s", errorParam)
			http.Error(w, "Authorization denied", http.StatusBadRequest)
			return
		}

		if code == "" || state == "" {
			http.Error(w, "Missing code or state", http.StatusBadRequest)
			return
		}

		// Verify state
		oauthStatesMu.Lock()
		oauthState, ok := oauthStates[state]
		if ok {
			delete(oauthStates, state)
		}
		oauthStatesMu.Unlock()

		if !ok {
			log.Printf("[google-oauth] Invalid state: %s", state)
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		// Check state expiration (10 minutes)
		if time.Since(oauthState.CreatedAt) > 10*time.Minute {
			http.Error(w, "State expired", http.StatusBadRequest)
			return
		}

		// Exchange code for tokens
		tokens, err := exchangeGoogleCode(deps.Config, code)
		if err != nil {
			log.Printf("[google-oauth] Failed to exchange code: %v", err)
			http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
			return
		}

		// Get user email from Google userinfo
		userEmail, err := getGoogleUserEmail(tokens.AccessToken)
		if err != nil {
			log.Printf("[google-oauth] Failed to get user email: %v", err)
			// Continue without email, not critical
		}

		// Save credentials
		creds := &credentials.UserCredentials{
			UserID:       oauthState.UserID,
			Provider:     "google",
			Email:        userEmail,
			AccessToken:  tokens.AccessToken,
			RefreshToken: tokens.RefreshToken,
			TokenType:    tokens.TokenType,
			ExpiresAt:    time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
			Scopes:       googleScopes,
		}

		if err := deps.CredService.SaveCredentials(creds); err != nil {
			log.Printf("[google-oauth] Failed to save credentials: %v", err)
			http.Error(w, "Failed to save credentials", http.StatusInternalServerError)
			return
		}

		log.Printf("[google-oauth] Successfully connected Google for user %d, email: %s", oauthState.UserID, userEmail)

		// Sync email to users table
		if userEmail != "" {
			_, err := deps.CredService.DB().Exec(
				`UPDATE users SET email = $1 WHERE telegram_id = $2 AND (email IS NULL OR email = '' OR email LIKE '%@duq.local')`,
				userEmail, oauthState.UserID,
			)
			if err != nil {
				log.Printf("[google-oauth] Failed to sync email to users: %v", err)
			} else {
				log.Printf("[google-oauth] Synced email %s to users table", userEmail)
			}
		}

		// Send confirmation to Telegram
		if deps.SendMessage != nil {
			deps.SendMessage(oauthState.ChatID, "Google аккаунт успешно подключён! Теперь я могу работать с твоими календарём, почтой и задачами.")
		}

		// Show success page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Google Connected</title>
    <style>
        body { font-family: -apple-system, system-ui, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #1a1a2e; color: #eee; }
        .container { text-align: center; padding: 2rem; }
        h1 { color: #4CAF50; }
        p { color: #aaa; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Google Account Connected</h1>
        <p>You can close this window and return to Telegram.</p>
    </div>
</body>
</html>`))
	}
}

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func exchangeGoogleCode(cfg *config.Config, code string) (*googleTokenResponse, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {cfg.GoogleOAuth.ClientID},
		"client_secret": {cfg.GoogleOAuth.ClientSecret},
		"redirect_uri":  {cfg.GoogleOAuth.RedirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.Post(
		"https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed: %s", string(body))
	}

	var tokens googleTokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &tokens, nil
}

// getGoogleUserEmail fetches user email from Google userinfo endpoint
func getGoogleUserEmail(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("userinfo request failed: %s", string(body))
	}

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", fmt.Errorf("failed to decode userinfo: %w", err)
	}

	return userInfo.Email, nil
}

// RefreshGoogleToken refreshes an expired Google access token
func RefreshGoogleToken(cfg *config.Config, credService *credentials.Service, creds *credentials.UserCredentials) error {
	if creds.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"client_id":     {cfg.GoogleOAuth.ClientID},
		"client_secret": {cfg.GoogleOAuth.ClientSecret},
		"refresh_token": {creds.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.Post(
		"https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed: %s", string(body))
	}

	var tokens googleTokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Update credentials
	creds.AccessToken = tokens.AccessToken
	creds.ExpiresAt = time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	if tokens.RefreshToken != "" {
		creds.RefreshToken = tokens.RefreshToken
	}

	return credService.SaveCredentials(creds)
}

// CleanupExpiredStates removes expired OAuth states
func CleanupExpiredStates() {
	oauthStatesMu.Lock()
	defer oauthStatesMu.Unlock()

	for state, info := range oauthStates {
		if time.Since(info.CreatedAt) > 15*time.Minute {
			delete(oauthStates, state)
		}
	}
}

// GetOAuthLinkHandler returns a handler for getting OAuth link via API
func GetOAuthLinkHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.URL.Query().Get("user_id")
		chatIDStr := r.URL.Query().Get("chat_id")

		if userIDStr == "" || chatIDStr == "" {
			http.Error(w, "Missing user_id or chat_id", http.StatusBadRequest)
			return
		}

		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}

		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid chat_id", http.StatusBadRequest)
			return
		}

		oauthURL, err := GenerateOAuthURL(cfg, userID, chatID)
		if err != nil {
			log.Printf("[google-oauth] Failed to generate URL: %v", err)
			http.Error(w, "Failed to generate OAuth URL", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"url": oauthURL})
	}
}

// InitiateOAuthRequest is the request body for InitiateOAuthHandler
type InitiateOAuthRequest struct {
	UserID string `json:"user_id"` // Telegram user ID as string (from Duq)
}

// InitiateOAuthHandler handles OAuth initiation from Duq agent
// POST /api/oauth/google/initiate
// Body: { "user_id": "764733417" }
// Response: { "success": true, "message": "OAuth link sent to Telegram" }
func InitiateOAuthHandler(deps *GoogleOAuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req InitiateOAuthRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[google-oauth] Failed to decode request: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.UserID == "" {
			http.Error(w, "Missing user_id", http.StatusBadRequest)
			return
		}

		userID, err := strconv.ParseInt(req.UserID, 10, 64)
		if err != nil {
			http.Error(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}

		// For Telegram, chatID is same as userID for private messages
		chatID := userID

		// Check if already connected
		if deps.CredService != nil {
			creds, err := deps.CredService.GetCredentials(userID, "google")
			if err != nil {
				log.Printf("[google-oauth] Error checking credentials: %v", err)
			} else if creds != nil {
				// Already connected
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":   true,
					"connected": true,
					"message":   "Google аккаунт уже подключён",
				})
				return
			}
		}

		// Generate OAuth URL
		oauthURL, err := GenerateOAuthURL(deps.Config, userID, chatID)
		if err != nil {
			log.Printf("[google-oauth] Failed to generate OAuth URL: %v", err)
			http.Error(w, "Failed to generate OAuth URL", http.StatusInternalServerError)
			return
		}

		// Send OAuth link to user via Telegram
		if deps.SendMessage != nil {
			msg := fmt.Sprintf("Для подключения Google аккаунта перейди по ссылке:\n\n%s\n\nПосле авторизации я смогу работать с твоими:\n• Календарём\n• Почтой\n• Задачами\n• Google Drive", oauthURL)
			if err := deps.SendMessage(chatID, msg); err != nil {
				log.Printf("[google-oauth] Failed to send Telegram message: %v", err)
				http.Error(w, "Failed to send OAuth link to Telegram", http.StatusInternalServerError)
				return
			}
		}

		log.Printf("[google-oauth] Initiated OAuth for user %d", userID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"connected": false,
			"message":   "OAuth link sent to Telegram",
		})
	}
}
