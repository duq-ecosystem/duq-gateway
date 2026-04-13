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
	UserID  string `json:"user_id"`
	Message string `json:"message"`
	IsVoice bool   `json:"is_voice,omitempty"`
	ChatID  int64  `json:"chat_id,omitempty"`
	Source  string `json:"source,omitempty"` // telegram, mobile, api
}

// APIResponse - unified response for message API
type APIResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// APIDeps - dependencies for unified API
type APIDeps struct {
	Config         *config.Config
	DBClient       *db.Client
	SessionService SessionServiceInterface
	QueueClient    *queue.Client
	CredService    CredentialServiceInterface
	RBACService    RBACServiceInterface
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

	// Build task
	// NOTE: ConversationID and history removed — Duq manages these
	task := &queue.Task{
		UserID:      req.UserID,
		Type:        "message",
		Priority:    50,
		CallbackURL: callbackURL,
		Payload: map[string]interface{}{
			"message":        req.Message,
			"output_channel": "telegram",
			"allowed_tools":  allowedTools,
			// NOTE: History removed — Duq loads from DB
			"user_preferences": map[string]string{
				"timezone":           prefs.Timezone,
				"preferred_language": prefs.PreferredLanguage,
			},
			"gws_credentials": gwsCreds,
		},
		RequestMetadata: map[string]interface{}{
			"chat_id":    req.ChatID,
			"user_email": userEmail,
			"is_voice":   req.IsVoice,
			"source":     req.Source,
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
