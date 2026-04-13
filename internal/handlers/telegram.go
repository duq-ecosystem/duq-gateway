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
	"duq-gateway/internal/credentials"
	"duq-gateway/internal/queue"
)

// refreshGoogleTokenIfNeeded checks if token is expired and refreshes it
func refreshGoogleTokenIfNeeded(cfg *config.Config, credService CredentialServiceInterface, creds *credentials.UserCredentials) error {
	if creds == nil || !creds.IsExpired() {
		return nil
	}

	if creds.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	log.Printf("[telegram] Token expired for user %d, refreshing...", creds.UserID)

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

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	// Update credentials
	creds.AccessToken = tokenResp.AccessToken
	creds.TokenType = tokenResp.TokenType
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// Save updated credentials
	if err := credService.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save refreshed credentials: %w", err)
	}

	log.Printf("[telegram] Token refreshed successfully for user %d", creds.UserID)
	return nil
}

// TelegramWithDeps creates a handler with full dependencies
func TelegramWithDeps(deps *TelegramDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var update TelegramUpdateFull
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			log.Printf("[telegram] Failed to decode update: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Handle callback queries (button clicks)
		if update.CallbackQuery != nil {
			handleCallbackQuery(w, update.CallbackQuery, deps)
			return
		}

		if update.Message == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		msg := update.Message

		if msg.From != nil && msg.From.IsBot {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Check if this is a voice message
		isVoice := msg.Voice != nil || msg.Audio != nil

		// Get message text
		text := msg.Text

		// Handle commands
		if strings.HasPrefix(text, "/") {
			handleTelegramCommand(w, msg, text, deps)
			return
		}

		if text == "" {
			if msg.Voice != nil {
				transcribed, err := transcribeVoice(deps.Config, msg.Voice.FileID)
				if err != nil {
					log.Printf("[telegram] STT failed: %v", err)
					text = "[Voice message - STT failed]"
				} else {
					text = transcribed
					log.Printf("[telegram] STT result: %s", truncateStr(text, 100))
				}
			} else if msg.Audio != nil {
				transcribed, err := transcribeVoice(deps.Config, msg.Audio.FileID)
				if err != nil {
					log.Printf("[telegram] STT failed for audio: %v", err)
					text = "[Audio file - STT failed]"
				} else {
					text = transcribed
				}
			} else if len(msg.Photo) > 0 {
				text = "[Photo]"
			} else if msg.Document != nil {
				text = "[Document]"
			} else {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		userID := formatTelegramUserID(msg.Chat.ID)
		telegramID := msg.Chat.ID

		log.Printf("[telegram] Message from %s (chat %d): %s",
			formatUserName(msg.From), msg.Chat.ID, truncateStr(text, 50))

		// Build chat options
		var opts chatOptions

		// Get allowed tools from RBAC if available
		if deps.RBACService != nil {
			// Ensure user exists
			username := ""
			firstName := ""
			lastName := ""
			if msg.From != nil {
				username = msg.From.Username
				firstName = msg.From.FirstName
				lastName = msg.From.LastName
			}
			deps.RBACService.EnsureUser(telegramID, username, firstName, lastName)

			tools, err := deps.RBACService.GetAllowedTools(telegramID)
			if err != nil {
				log.Printf("[telegram] RBAC error: %v", err)
			} else {
				opts.AllowedTools = tools
				log.Printf("[telegram] User %d has %d allowed tools", telegramID, len(tools))
			}
		}

		// NOTE: Conversation history is now managed by Duq backend.
		// Gateway is pass-through — no session management here.
		// Duq loads history from DB and saves messages automatically.

		// Get user preferences from database
		if deps.DBClient != nil {
			prefs := deps.DBClient.GetUserPreferencesByTelegramID(telegramID)
			opts.UserPreferences = &UserPreferences{
				Timezone:          prefs.Timezone,
				PreferredLanguage: prefs.PreferredLanguage,
			}
			log.Printf("[telegram] User %d preferences: tz=%s, lang=%s",
				telegramID, prefs.Timezone, prefs.PreferredLanguage)
		}

		// Get GWS credentials if available
		var userEmail string
		if deps.CredService != nil {
			creds, err := deps.CredService.GetCredentials(telegramID, "google")
			if err != nil {
				log.Printf("[telegram] Error fetching GWS credentials: %v", err)
			} else if creds != nil {
				// Auto-refresh if token expired
				if err := refreshGoogleTokenIfNeeded(deps.Config, deps.CredService, creds); err != nil {
					log.Printf("[telegram] Failed to refresh token: %v", err)
				}
				opts.GWSCredentials = map[string]string{
					"access_token":  creds.AccessToken,
					"refresh_token": creds.RefreshToken,
					"token_type":    creds.TokenType,
				}
				userEmail = creds.Email
				log.Printf("[telegram] User %d has GWS credentials (email=%s)", telegramID, userEmail)
			}
		}

		// Push directly to Redis queue - Duq worker will pick it up
		callbackURL := fmt.Sprintf("http://%s/api/duq/callback", deps.Config.GatewayHost)

		inputType := "text"
		if isVoice {
			inputType = "voice"
		}

		// NOTE: History is no longer sent to Duq.
		// Duq manages conversation history in its own database.

		// Build user preferences for payload
		var userPrefs map[string]string
		if opts.UserPreferences != nil {
			userPrefs = map[string]string{
				"timezone":           opts.UserPreferences.Timezone,
				"preferred_language": opts.UserPreferences.PreferredLanguage,
			}
		}

		task := &queue.Task{
			UserID:      userID,
			Type:        "message",
			Priority:    50,
			CallbackURL: callbackURL,
			// NOTE: ConversationID is managed by Duq, not Gateway
			Payload: map[string]interface{}{
				"message":          text,
				"output_channel":   "telegram",
				"allowed_tools":    opts.AllowedTools,
				// NOTE: History removed — Duq loads from DB
				"user_preferences": userPrefs,
				"gws_credentials":  opts.GWSCredentials,
			},
			RequestMetadata: map[string]interface{}{
				"chat_id":    msg.Chat.ID,
				"user_email": userEmail,
				"is_voice":   isVoice,
				"input_type": inputType,
			},
		}

		_, err := deps.QueueClient.Push(r.Context(), task)
		if err != nil {
			log.Printf("[telegram] Failed to push to Redis queue: %v", err)
			SendTelegramMessage(deps.Config, msg.Chat.ID, "⚠️ Сервис временно недоступен. Попробуй позже.")
		} else {
			log.Printf("[telegram] Message pushed to Redis queue for user %s", userID)
		}

		w.WriteHeader(http.StatusOK)
	}
}

// TelegramSend creates a handler for sending Telegram messages
// Used by duq scheduler for morning messages
func TelegramSend(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TelegramSendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[telegram/send] Failed to decode request: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.ChatID == 0 || req.Text == "" {
			http.Error(w, "chat_id and text required", http.StatusBadRequest)
			return
		}

		log.Printf("[telegram/send] Sending to %d: %s", req.ChatID, truncateStr(req.Text, 50))

		if req.Voice {
			// Voice + text response (same logic as voice input response)
			const maxCaptionLen = 1024

			if len(req.Text) <= maxCaptionLen {
				// Short: voice with caption
				if err := sendTelegramVoiceWithCaption(cfg, req.ChatID, req.Text, req.Text); err != nil {
					log.Printf("[telegram/send] Failed to send voice with caption: %v", err)
					http.Error(w, "Failed to send voice", http.StatusInternalServerError)
					return
				}
			} else {
				// Long: text first, then voice without caption
				if err := SendTelegramMessage(cfg, req.ChatID, req.Text); err != nil {
					log.Printf("[telegram/send] Failed to send text: %v", err)
					http.Error(w, "Failed to send text", http.StatusInternalServerError)
					return
				}
				if err := sendTelegramVoiceWithCaption(cfg, req.ChatID, req.Text, ""); err != nil {
					log.Printf("[telegram/send] Failed to send voice: %v", err)
					http.Error(w, "Failed to send voice", http.StatusInternalServerError)
					return
				}
			}
		} else {
			// Text only
			if err := SendTelegramMessage(cfg, req.ChatID, req.Text); err != nil {
				log.Printf("[telegram/send] Failed to send text: %v", err)
				http.Error(w, "Failed to send text", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}
}
