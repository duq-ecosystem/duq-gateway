package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
	"duq-gateway/internal/queue"
)

// MessageRequest - UNIFIED request format for ALL clients
type MessageRequest struct {
	UserID      string `json:"user_id"`
	Message     string `json:"message"`
	IsVoice     bool   `json:"is_voice,omitempty"`
	VoiceData   string `json:"voice_data,omitempty"`   // Base64 encoded audio
	VoiceFormat string `json:"voice_format,omitempty"` // wav, ogg, mp3
	ChatID      int64  `json:"chat_id,omitempty"`
	Source      string `json:"source,omitempty"` // telegram, android, api
}

// APIResponse - unified response for message API
type APIResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// APIDeps - dependencies for unified API
type APIDeps struct {
	Config      *config.Config
	DBClient    *db.Client
	QueueClient *queue.Client
	CredService CredentialServiceInterface
	RBACService RBACServiceInterface
}

// ProcessMessage - THE CORE LOGIC for all message processing
// Called by HTTP handler AND Telegram adapter
func ProcessMessage(ctx context.Context, deps *APIDeps, req *MessageRequest) (*APIResponse, error) {
	if req.UserID == "" || req.Message == "" {
		return &APIResponse{Error: "user_id and message required"}, fmt.Errorf("missing fields")
	}

	// Parse telegram_id
	var telegramID int64
	fmt.Sscanf(req.UserID, "%d", &telegramID)
	if req.ChatID == 0 {
		req.ChatID = telegramID
	}
	if req.Source == "" {
		req.Source = "api"
	}

	log.Printf("[api] user=%s source=%s msg=%q", req.UserID, req.Source, truncMsg(req.Message, 50))

	// Get or create user in DB to get db_user_id
	var dbUserID int64
	user, err := deps.DBClient.GetUserByTelegramID(telegramID)
	if err != nil {
		log.Printf("[api] Error getting user: %v", err)
	}
	if user != nil {
		dbUserID = user.ID
	} else {
		// Create user if not exists
		newUser, err := deps.DBClient.CreateUserFromTelegram(telegramID, "", "", "")
		if err != nil {
			log.Printf("[api] Error creating user: %v", err)
		} else {
			dbUserID = newUser.ID
		}
	}

	// Ensure user exists in RBAC
	if deps.RBACService != nil {
		deps.RBACService.EnsureUser(telegramID, "", "", "")
	}

	// NOTE: Conversation history is now managed by Duq backend.
	// Gateway is pass-through — no session management here.
	// Duq loads history from DB and saves messages automatically.

	// User prefs
	prefs := deps.DBClient.GetUserPreferencesByTelegramID(telegramID)

	// Allowed tools from RBAC
	var allowedTools []string
	if deps.RBACService != nil {
		tools, err := deps.RBACService.GetAllowedTools(telegramID)
		if err == nil {
			allowedTools = tools
		}
	}

	// GWS credentials
	var gwsCreds map[string]string
	var userEmail string
	if deps.CredService != nil {
		if creds, err := deps.CredService.GetCredentials(telegramID, "google"); err == nil && creds != nil {
			gwsCreds = map[string]string{
				"access_token":  creds.AccessToken,
				"refresh_token": creds.RefreshToken,
				"token_type":    creds.TokenType,
			}
			userEmail = creds.Email
		}
	}

	// NOTE: Message saving removed — Duq handles this now

	// Callback URL
	callbackURL := fmt.Sprintf("http://%s/api/duq/callback", deps.Config.GatewayHost)

	// Build task payload
	// Note: output_channel not set here - Duq agent decides where to respond
	// via select_output_channel tool based on context
	payload := map[string]interface{}{
		"message":       req.Message,
		"allowed_tools": allowedTools,
		"user_preferences": map[string]string{
			"timezone":           prefs.Timezone,
			"preferred_language": prefs.PreferredLanguage,
		},
		"gws_credentials": gwsCreds,
	}

	// Add voice data if present (for transcription by Duq)
	if req.VoiceData != "" {
		payload["voice_data"] = req.VoiceData
		if req.VoiceFormat != "" {
			payload["voice_format"] = req.VoiceFormat
		} else {
			payload["voice_format"] = "wav" // default
		}
	}

	// Build task
	task := &queue.Task{
		UserID:      req.UserID,
		Type:        "message",
		Priority:    50,
		CallbackURL: callbackURL,
		Payload:     payload,
		RequestMetadata: map[string]interface{}{
			"chat_id":     req.ChatID,
			"db_user_id":  dbUserID,
			"user_email":  userEmail,
			"is_voice":    req.IsVoice,
			"source":      req.Source,
		},
	}

	// Push to queue
	taskID, err := deps.QueueClient.Push(ctx, task)
	if err != nil {
		log.Printf("[api] queue error: %v", err)
		return &APIResponse{Error: "queue unavailable"}, err
	}

	log.Printf("[api] queued: task=%s user=%s source=%s", taskID, req.UserID, req.Source)
	return &APIResponse{TaskID: taskID, Status: "queued"}, nil
}

// UnifiedAPI - HTTP handler for POST /api/message
// ALL clients use this: mobile, web, api
func UnifiedAPI(deps *APIDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(APIResponse{Error: "invalid json"})
			return
		}

		// Auto-populate user_id from auth context if not provided
		// This allows authenticated clients (Android, web) to omit user_id
		if req.UserID == "" {
			// Context values may be int64 (from keycloak middleware) or string
			if telegramID, ok := r.Context().Value("telegram_id").(int64); ok && telegramID != 0 {
				req.UserID = fmt.Sprintf("%d", telegramID)
				log.Printf("[api] Using telegram_id from context: %s", req.UserID)
			} else if telegramIDStr, ok := r.Context().Value("telegram_id").(string); ok && telegramIDStr != "" {
				req.UserID = telegramIDStr
				log.Printf("[api] Using telegram_id (string) from context: %s", req.UserID)
			} else if userID, ok := r.Context().Value("user_id").(int64); ok && userID != 0 {
				req.UserID = fmt.Sprintf("%d", userID)
				log.Printf("[api] Using user_id from context: %s", req.UserID)
			} else if userIDStr, ok := r.Context().Value("user_id").(string); ok && userIDStr != "" {
				req.UserID = userIDStr
				log.Printf("[api] Using user_id (string) from context: %s", req.UserID)
			}
		}

		// Set source to android if authenticated via Keycloak and source not set
		if req.Source == "" {
			if _, ok := r.Context().Value("keycloak_username").(string); ok {
				req.Source = "android"
			}
		}

		resp, err := ProcessMessage(r.Context(), deps, &req)

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
		json.NewEncoder(w).Encode(resp)
	}
}

func truncMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TaskStatusResponse - response for task status polling
type TaskStatusResponse struct {
	TaskID   string                 `json:"task_id"`
	Status   string                 `json:"status"`
	Response map[string]interface{} `json:"response,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// GetTaskStatus - HTTP handler for GET /api/task/{id}
// Used by mobile clients to poll for task results
func GetTaskStatus(deps *APIDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("id")
		if taskID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TaskStatusResponse{Error: "task_id required"})
			return
		}

		ctx := r.Context()
		resp := TaskStatusResponse{TaskID: taskID}

		// First check if response exists (Duq worker stores here when done)
		if response, err := deps.QueueClient.GetTaskResponse(ctx, taskID); err == nil && response != nil {
			// Response exists - task completed
			if success, ok := response["success"].(bool); ok && success {
				resp.Status = "COMPLETED"
				// Extract result which contains voice_data
				if result, ok := response["result"].(map[string]interface{}); ok {
					resp.Response = result
				}
			} else {
				resp.Status = "FAILED"
				if errMsg, ok := response["error"].(string); ok {
					resp.Error = errMsg
				}
			}
		} else {
			// No response yet - check task status in hash
			status, err := deps.QueueClient.GetTaskStatus(ctx, taskID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(TaskStatusResponse{TaskID: taskID, Error: "task not found"})
				return
			}
			resp.Status = status
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}
