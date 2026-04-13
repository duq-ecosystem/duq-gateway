package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"duq-gateway/internal/session"
)

// MessageResponse represents a message in API responses
type MessageResponse struct {
	ID              int64     `json:"id"`
	ConversationID  string    `json:"conversation_id"`
	Role            string    `json:"role"` // "user" or "assistant"
	Content         string    `json:"content"`
	HasAudio        bool      `json:"has_audio"`
	AudioDurationMs int       `json:"audio_duration_ms,omitempty"`
	Waveform        []float64 `json:"waveform,omitempty"`
	CreatedAt       int64     `json:"created_at"` // Unix timestamp
}

// ConversationMessages handles GET /api/conversations/{id}/messages
// Returns messages for a conversation with optional pagination
func ConversationMessages(sessionService *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Get conversation ID from path
		conversationID := r.PathValue("id")
		if conversationID == "" {
			http.Error(w, "conversation_id is required", http.StatusBadRequest)
			return
		}

		// Parse limit from query parameter (default: 50)
		limit := 50
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
				limit = parsedLimit
			}
		}

		// Get messages from session service
		messages, err := sessionService.GetRecentMessages(conversationID, limit)
		if err != nil {
			log.Printf("[messages] Failed to get messages for conversation %s: %v", conversationID, err)
			http.Error(w, "Failed to fetch messages", http.StatusInternalServerError)
			return
		}

		// Get audio metadata for messages that have audio
		audioMap, err := sessionService.GetMessagesAudio(conversationID)
		if err != nil {
			log.Printf("[messages] Warning: failed to get audio metadata: %v", err)
			audioMap = make(map[int64]session.AudioMetadata)
		}

		// Convert to response format
		response := make([]MessageResponse, len(messages))
		for i, msg := range messages {
			msgResp := MessageResponse{
				ID:             msg.ID, // Use actual database ID
				ConversationID: conversationID,
				Role:           msg.Role,
				Content:        msg.Content,
				CreatedAt:      msg.CreatedAt.Unix(),
			}

			// Add audio metadata if available
			if audio, ok := audioMap[msg.ID]; ok {
				msgResp.HasAudio = true
				msgResp.AudioDurationMs = audio.DurationMs
				msgResp.Waveform = audio.Waveform
			}

			response[i] = msgResp
		}

		log.Printf("[messages] Returning %d messages for conversation %s (user %d)", len(response), conversationID, telegramID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// MessageAudio handles GET /api/messages/{id}/audio
// Returns OGG audio file for a message
func MessageAudio(sessionService *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Get message ID from path
		messageIDStr := r.PathValue("id")
		if messageIDStr == "" {
			http.Error(w, "message_id is required", http.StatusBadRequest)
			return
		}

		messageID, err := strconv.ParseInt(messageIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid message_id", http.StatusBadRequest)
			return
		}

		// Get audio data from database
		audioData, meta, err := sessionService.GetMessageAudio(messageID)
		if err != nil {
			log.Printf("[messages] Failed to get audio for message %d: %v", messageID, err)
			http.Error(w, "Audio not found", http.StatusNotFound)
			return
		}

		log.Printf("[messages] Serving audio for message %d (user %d, duration %dms, size %d bytes)",
			messageID, telegramID, meta.DurationMs, len(audioData))

		w.Header().Set("Content-Type", "audio/ogg")
		w.Header().Set("Content-Length", strconv.Itoa(len(audioData)))
		w.Header().Set("X-Audio-Duration-Ms", strconv.Itoa(meta.DurationMs))
		w.Write(audioData)
	}
}
