package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"duq-gateway/internal/session"
)

// ConversationResponse represents a conversation in API responses
type ConversationResponse struct {
	ID            string `json:"id"`
	UserID        int64  `json:"user_id"`
	Title         string `json:"title,omitempty"`
	StartedAt     int64  `json:"started_at"`      // Unix timestamp
	LastMessageAt int64  `json:"last_message_at"` // Unix timestamp
	IsActive      bool   `json:"is_active"`
}

// CreateConversationRequest for creating a new conversation
type CreateConversationRequest struct {
	Title string `json:"title,omitempty"`
}

// UpdateConversationRequest for updating conversation title
type UpdateConversationRequest struct {
	Title string `json:"title"`
}

// ConversationsList handles GET /api/conversations
// Returns list of user's conversations
func ConversationsList(sessionService *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get telegram_id from context (set by UnifiedAuth middleware)
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Get user's conversations
		conversations, err := sessionService.GetUserConversations(telegramID, 20)
		if err != nil {
			log.Printf("[conversations] Failed to get conversations for user %d: %v", telegramID, err)
			http.Error(w, "Failed to fetch conversations", http.StatusInternalServerError)
			return
		}

		// Convert to response format
		response := make([]ConversationResponse, len(conversations))
		for i, conv := range conversations {
			response[i] = ConversationResponse{
				ID:            conv.ID,
				UserID:        conv.UserID,
				Title:         conv.Title,
				StartedAt:     conv.StartedAt.Unix(),
				LastMessageAt: conv.LastMessageAt.Unix(),
				IsActive:      conv.IsActive,
			}
		}

		log.Printf("[conversations] Returning %d conversations for user %d", len(response), telegramID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// CreateConversation handles POST /api/conversations
// Creates a new conversation for the user
func CreateConversation(sessionService *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telegramID, ok := r.Context().Value("telegram_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req CreateConversationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Create conversation
		conv, err := sessionService.GetOrCreateConversation(telegramID)
		if err != nil {
			log.Printf("[conversations] Failed to create conversation for user %d: %v", telegramID, err)
			http.Error(w, "Failed to create conversation", http.StatusInternalServerError)
			return
		}

		// Set title if provided
		if req.Title != "" {
			if err := sessionService.SetConversationTitle(conv.ID, req.Title); err != nil {
				log.Printf("[conversations] Failed to set title for conversation %s: %v", conv.ID, err)
				// Don't fail the request, title is optional
			} else {
				conv.Title = req.Title
			}
		}

		log.Printf("[conversations] Created conversation %s for user %d", conv.ID, telegramID)

		response := ConversationResponse{
			ID:            conv.ID,
			UserID:        conv.UserID,
			Title:         conv.Title,
			StartedAt:     conv.StartedAt.Unix(),
			LastMessageAt: conv.LastMessageAt.Unix(),
			IsActive:      conv.IsActive,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// UpdateConversation handles PUT /api/conversations/{id}
// Updates conversation metadata (currently only title)
func UpdateConversation(sessionService *session.Service) http.HandlerFunc {
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

		var req UpdateConversationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.Title == "" {
			http.Error(w, "title is required", http.StatusBadRequest)
			return
		}

		// Update title
		if err := sessionService.SetConversationTitle(conversationID, req.Title); err != nil {
			log.Printf("[conversations] Failed to update conversation %s: %v", conversationID, err)
			http.Error(w, "Failed to update conversation", http.StatusInternalServerError)
			return
		}

		log.Printf("[conversations] Updated conversation %s for user %d", conversationID, telegramID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// EndConversation handles DELETE /api/conversations/{id}
// Marks conversation as inactive
func EndConversation(sessionService *session.Service) http.HandlerFunc {
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

		// End conversation
		if err := sessionService.EndConversation(conversationID); err != nil {
			log.Printf("[conversations] Failed to end conversation %s: %v", conversationID, err)
			http.Error(w, "Failed to end conversation", http.StatusInternalServerError)
			return
		}

		log.Printf("[conversations] Ended conversation %s for user %d", conversationID, telegramID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}
