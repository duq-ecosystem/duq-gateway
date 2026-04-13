package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
	"duq-gateway/internal/session"
)

// VoiceResponse represents the voice endpoint response
type VoiceResponse struct {
	Text  string `json:"text"`
	Audio string `json:"audio"` // base64 encoded OGG
}

// VoiceErrorResponse represents an error response
type VoiceErrorResponse struct {
	Error string `json:"error"`
}

// DuqVoiceResponse represents Duq /api/voice response
type DuqVoiceResponse struct {
	Transcription string `json:"transcription"`
	Response      string `json:"response"`
	UserID        string `json:"user_id"`
	OutputType    string `json:"output_type"`
	VoicePriority string `json:"voice_priority"`
	VoiceData     string `json:"voice_data"` // base64 OGG
	VoiceFormat   string `json:"voice_format"`
}

// VoiceDeps contains dependencies for the voice handler
type VoiceDeps struct {
	Config         *config.Config
	DBClient       *db.Client
	SessionService *session.Service
}

// Voice handles voice message processing.
// Proxies audio to Duq /api/voice which does:
// - STT (whisper-stt)
// - Agent processing
// - TTS (edge-tts)
// - Returns OGG audio
func Voice(deps *VoiceDeps) http.HandlerFunc {
	duqURL := deps.Config.DuqURL
	if duqURL == "" {
		duqURL = "http://localhost:8081"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Get telegram_id from context (set by MobileAuth middleware)
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok {
			sendVoiceError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID := fmt.Sprintf("%d", telegramID)
		log.Printf("[voice] Request from user %s", userID)

		// Parse multipart form (max 20MB)
		if err := r.ParseMultipartForm(20 << 20); err != nil {
			sendVoiceError(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Get audio file
		file, header, err := r.FormFile("audio")
		if err != nil {
			sendVoiceError(w, "Audio file required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		log.Printf("[voice] Received audio: %s, size=%d", header.Filename, header.Size)

		// Read audio bytes
		audioBytes, err := io.ReadAll(file)
		if err != nil {
			sendVoiceError(w, "Failed to read audio", http.StatusInternalServerError)
			return
		}

		// Get conversation ID for history
		convID, err := deps.SessionService.GetOrCreateConversationID(telegramID)
		if err != nil {
			log.Printf("[voice] Failed to get conversation: %v", err)
			convID = ""
		}

		// Get user preferences
		prefs := deps.DBClient.GetUserPreferencesByTelegramID(telegramID)

		// Proxy to Duq /api/voice
		duqResp, err := proxyToDuqVoice(
			duqURL,
			audioBytes,
			header.Filename,
			userID,
			convID,
			prefs.Timezone,
			prefs.PreferredLanguage,
		)
		if err != nil {
			log.Printf("[voice] Duq /api/voice failed: %v", err)
			sendVoiceError(w, "Voice processing failed", http.StatusInternalServerError)
			return
		}

		log.Printf("[voice] Duq response: transcription=%q, response=%q",
			truncate(duqResp.Transcription, 50), truncate(duqResp.Response, 50))

		// Save messages to session
		if convID != "" {
			if err := deps.SessionService.SaveMessageSimple(convID, "user", duqResp.Transcription); err != nil {
				log.Printf("[voice] Failed to save user message: %v", err)
			}
			if err := deps.SessionService.SaveMessageSimple(convID, "assistant", duqResp.Response); err != nil {
				log.Printf("[voice] Failed to save assistant message: %v", err)
			}
		}

		// Send to Telegram for cross-channel visibility
		go func() {
			userMsg := fmt.Sprintf("📱 *[Mobile App]*\n\n%s", duqResp.Transcription)
			if err := SendTelegramMessage(deps.Config, telegramID, userMsg); err != nil {
				log.Printf("[voice] Failed to send user message to Telegram: %v", err)
			}

			cleanResponse := cleanAgentResponse(duqResp.Response)
			assistantMsg := fmt.Sprintf("🤖 *[Duq]*\n\n%s", cleanResponse)
			if err := SendTelegramMessage(deps.Config, telegramID, assistantMsg); err != nil {
				log.Printf("[voice] Failed to send assistant response to Telegram: %v", err)
			}
		}()

		// Return response to mobile client
		resp := VoiceResponse{
			Text:  cleanAgentResponse(duqResp.Response),
			Audio: duqResp.VoiceData, // Already base64 OGG from Duq
		}

		log.Printf("[voice] Success: text=%d chars, audio=%d bytes (base64)",
			len(resp.Text), len(resp.Audio))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// proxyToDuqVoice sends audio to Duq /api/voice
func proxyToDuqVoice(
	duqURL string,
	audioBytes []byte,
	filename string,
	userID string,
	conversationID string,
	timezone string,
	preferredLanguage string,
) (*DuqVoiceResponse, error) {
	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add audio file
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return nil, fmt.Errorf("failed to write audio: %w", err)
	}

	// Add form fields
	writer.WriteField("user_id", userID)
	if conversationID != "" {
		writer.WriteField("conversation_id", conversationID)
	}
	if timezone != "" {
		writer.WriteField("timezone", timezone)
	}
	if preferredLanguage != "" {
		writer.WriteField("preferred_language", preferredLanguage)
	}

	writer.Close()

	// Send request to Duq
	url := duqURL + "/api/voice"
	log.Printf("[voice] POST %s (audio=%d bytes)", url, len(audioBytes))

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(url, writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duq returned %d: %s", resp.StatusCode, string(body))
	}

	var duqResp DuqVoiceResponse
	if err := json.Unmarshal(body, &duqResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &duqResp, nil
}

func sendVoiceError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(VoiceErrorResponse{Error: message})
}

// truncate truncates string to max length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// cleanAgentResponse removes debug output lines from agent response
func cleanAgentResponse(response string) string {
	debugPrefixes := []string{
		"[duq-memory]",
		"[memory]",
		"[plugin]",
		"[debug]",
		"[DEBUG]",
	}

	lines := strings.Split(response, "\n")
	var cleaned []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isDebug := false
		for _, prefix := range debugPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				isDebug = true
				break
			}
		}
		if !isDebug {
			cleaned = append(cleaned, line)
		}
	}

	result := strings.Join(cleaned, "\n")
	result = strings.TrimSpace(result)

	return result
}
