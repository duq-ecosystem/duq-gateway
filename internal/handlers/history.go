package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
	"duq-gateway/internal/queue"
)

// HistoryDeps - dependencies for history handlers
type HistoryDeps struct {
	Config      *config.Config
	DBClient    *db.Client
	QueueClient *queue.Client
}

// ConversationResponse - matches Android app expectations
type ConversationResponse struct {
	ID            string `json:"id"`
	UserID        int64  `json:"user_id"`
	Title         string `json:"title"`
	StartedAt     int64  `json:"started_at"`
	LastMessageAt int64  `json:"last_message_at"`
	IsActive      bool   `json:"is_active"`
}

// MessageResponse - matches Android app expectations
type MessageResponse struct {
	ID              int64     `json:"id"`
	ConversationID  string    `json:"conversation_id"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	HasAudio        bool      `json:"has_audio"`
	AudioDurationMs *int      `json:"audio_duration_ms"`
	Waveform        []float64 `json:"waveform"`
	CreatedAt       int64     `json:"created_at"`
}

// GetConversations returns list of sessions (dates) as conversations
// GET /api/conversations
func GetConversations(deps *HistoryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user from context (set by KeycloakAuth middleware)
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok || telegramID == 0 {
			// Try as string
			if telegramIDStr, ok := r.Context().Value("telegram_id").(string); ok && telegramIDStr != "" {
				fmt.Sscanf(telegramIDStr, "%d", &telegramID)
			}
		}

		if telegramID == 0 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID := fmt.Sprintf("%d", telegramID)
		log.Printf("[history] Getting conversations for user %s", userID)

		ctx := r.Context()

		// Get sessions from Redis
		sessions, err := deps.QueueClient.GetHistorySessions(ctx, userID)
		if err != nil {
			log.Printf("[history] Error getting sessions: %v", err)
			http.Error(w, `{"error":"failed to get conversations"}`, http.StatusInternalServerError)
			return
		}

		// Sort sessions (dates) descending (newest first)
		sort.Sort(sort.Reverse(sort.StringSlice(sessions)))

		// Today's date for active check
		today := time.Now().UTC().Format("2006-01-02")

		// Convert to ConversationResponse
		var conversations []ConversationResponse
		for _, sessionID := range sessions {
			// Parse session date
			sessionDate, err := time.Parse("2006-01-02", sessionID)
			if err != nil {
				log.Printf("[history] Invalid session date: %s", sessionID)
				continue
			}

			// Get messages to determine last message time
			messages, _ := deps.QueueClient.GetHistoryMessages(ctx, userID, sessionID)
			var lastMessageAt int64
			if len(messages) > 0 {
				// Parse last message timestamp
				lastMsg := messages[len(messages)-1]
				if ts, err := time.Parse(time.RFC3339, lastMsg.Timestamp); err == nil {
					lastMessageAt = ts.Unix()
				}
			}
			if lastMessageAt == 0 {
				lastMessageAt = sessionDate.Unix()
			}

			conv := ConversationResponse{
				ID:            sessionID,
				UserID:        telegramID,
				Title:         formatDateTitle(sessionDate),
				StartedAt:     sessionDate.Unix(),
				LastMessageAt: lastMessageAt,
				IsActive:      sessionID == today,
			}
			conversations = append(conversations, conv)
		}

		// If no conversations exist, return empty array
		if conversations == nil {
			conversations = []ConversationResponse{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(conversations)
		log.Printf("[history] Returned %d conversations for user %s", len(conversations), userID)
	}
}

// GetMessages returns messages for a session
// GET /api/conversations/{session_id}/messages
func GetMessages(deps *HistoryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user from context
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok || telegramID == 0 {
			if telegramIDStr, ok := r.Context().Value("telegram_id").(string); ok && telegramIDStr != "" {
				fmt.Sscanf(telegramIDStr, "%d", &telegramID)
			}
		}

		if telegramID == 0 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		sessionID := r.PathValue("session_id")
		if sessionID == "" {
			http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
			return
		}

		userID := fmt.Sprintf("%d", telegramID)
		log.Printf("[history] Getting messages for user %s, session %s", userID, sessionID)

		ctx := r.Context()

		// Get messages from Redis
		historyMessages, err := deps.QueueClient.GetHistoryMessages(ctx, userID, sessionID)
		if err != nil {
			log.Printf("[history] Error getting messages: %v", err)
			http.Error(w, `{"error":"failed to get messages"}`, http.StatusInternalServerError)
			return
		}

		// Convert to MessageResponse
		var messages []MessageResponse
		for i, msg := range historyMessages {
			var createdAt int64
			if ts, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
				createdAt = ts.Unix()
			}

			resp := MessageResponse{
				ID:             int64(i + 1), // Sequential ID within session
				ConversationID: sessionID,
				Role:           msg.Role,
				Content:        msg.Content,
				HasAudio:       false,
				AudioDurationMs: nil,
				Waveform:       nil,
				CreatedAt:      createdAt,
			}
			messages = append(messages, resp)
		}

		// Return empty array if no messages
		if messages == nil {
			messages = []MessageResponse{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(messages)
		log.Printf("[history] Returned %d messages for session %s", len(messages), sessionID)
	}
}

// CreateConversation creates a new conversation (session)
// POST /api/conversations
func CreateConversation(deps *HistoryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user from context
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok || telegramID == 0 {
			if telegramIDStr, ok := r.Context().Value("telegram_id").(string); ok && telegramIDStr != "" {
				fmt.Sscanf(telegramIDStr, "%d", &telegramID)
			}
		}

		if telegramID == 0 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// New conversation = today's session
		today := time.Now().UTC()
		sessionID := today.Format("2006-01-02")

		conv := ConversationResponse{
			ID:            sessionID,
			UserID:        telegramID,
			Title:         formatDateTitle(today),
			StartedAt:     today.Unix(),
			LastMessageAt: today.Unix(),
			IsActive:      true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(conv)
		log.Printf("[history] Created conversation %s for user %d", sessionID, telegramID)
	}
}

// formatDateTitle formats a date for display
func formatDateTitle(t time.Time) string {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	sessionDay := t.Truncate(24 * time.Hour)

	if sessionDay.Equal(today) {
		return "Today"
	}
	if sessionDay.Equal(today.AddDate(0, 0, -1)) {
		return "Yesterday"
	}
	return t.Format("January 2, 2006")
}
