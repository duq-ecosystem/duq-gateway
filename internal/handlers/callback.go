package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"duq-gateway/internal/channels"
	"duq-gateway/internal/config"
	"duq-gateway/internal/duq"
	"duq-gateway/internal/session"
)

// CallbackDeps contains dependencies for the callback handler
type CallbackDeps struct {
	Config         *config.Config
	SessionService SessionServiceInterface
	ChannelRouter  *channels.Router
}

// DuqCallback handles callbacks from Duq TwoLevelWorker.
// When a queued task completes, Duq POSTs the result here.
func DuqCallback(deps *CallbackDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[callback] Failed to read body: %v", err)
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var payload duq.CallbackPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("[callback] Failed to decode payload: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		log.Printf("[callback] Received: task_id=%s, user_id=%s, success=%v",
			payload.TaskID, payload.UserID, payload.Success)

		// Extract response from result
		var response string
		var outputChannel string = "telegram"
		var voiceData []byte
		var voiceFormat string

		if payload.Success && payload.Result != nil {
			if resp, ok := payload.Result["response"].(string); ok {
				response = resp
			}
			if ch, ok := payload.Result["channel"].(string); ok {
				outputChannel = ch
			}
			// Extract voice data from Duq worker (base64 encoded)
			if vd, ok := payload.Result["voice_data"].(string); ok && vd != "" {
				decoded, err := base64.StdEncoding.DecodeString(vd)
				if err != nil {
					log.Printf("[callback] Failed to decode voice_data: %v", err)
				} else {
					voiceData = decoded
					log.Printf("[callback] Voice data extracted: %d bytes", len(voiceData))
				}
			}
			if vf, ok := payload.Result["voice_format"].(string); ok {
				voiceFormat = vf
			}
		} else if !payload.Success {
			response = "Error processing request: " + payload.Error
		}

		if response == "" {
			log.Printf("[callback] Empty response, skipping delivery")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Extract chat_id from request_metadata
		var chatID int64
		if payload.RequestMetadata != nil {
			if cid, ok := payload.RequestMetadata["chat_id"].(float64); ok {
				chatID = int64(cid)
			}
		}

		// Fallback: parse user_id as chat_id (for Telegram, user_id IS chat_id)
		if chatID == 0 {
			var uid int64
			if err := json.Unmarshal([]byte(payload.UserID), &uid); err == nil {
				chatID = uid
			}
		}

		if chatID == 0 {
			log.Printf("[callback] Cannot determine chat_id, skipping delivery")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Get user email from metadata if available
		var userEmail string
		if payload.RequestMetadata != nil {
			if email, ok := payload.RequestMetadata["user_email"].(string); ok {
				userEmail = email
			}
		}

		// Check if this was a voice message
		isVoice := false
		if payload.RequestMetadata != nil {
			if v, ok := payload.RequestMetadata["is_voice"].(bool); ok {
				isVoice = v
			}
		}

		// Save assistant response to session
		if deps.SessionService != nil {
			if convID, ok := payload.RequestMetadata["conversation_id"].(string); ok && convID != "" {
				if err := deps.SessionService.SaveMessageSimple(convID, "assistant", response); err != nil {
					log.Printf("[callback] Failed to save assistant message: %v", err)
				}
			}
		}

		// Route response via channel router
		go func() {
			ctx := &channels.ResponseContext{
				ChatID:      chatID,
				UserEmail:   userEmail,
				Response:    response,
				IsVoice:     isVoice,
				VoiceData:   voiceData,
				VoiceFormat: voiceFormat,
			}

			if deps.ChannelRouter != nil {
				if err := deps.ChannelRouter.Route(outputChannel, ctx); err != nil {
					log.Printf("[callback] Channel routing failed: %v", err)
				}
			} else {
				// Fallback: send to Telegram directly
				if err := SendTelegramMessage(deps.Config, chatID, response); err != nil {
					log.Printf("[callback] Failed to send Telegram message: %v", err)
				}
			}
		}()

		executionTime := "unknown"
		if payload.ExecutionTimeMs != nil {
			executionTime = fmt.Sprintf("%dms", *payload.ExecutionTimeMs)
		}
		log.Printf("[callback] Delivered task_id=%s to channel=%s (exec_time=%s)",
			payload.TaskID, outputChannel, executionTime)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}
}

// NewCallbackDeps creates CallbackDeps from existing services
func NewCallbackDeps(
	cfg *config.Config,
	sessionService SessionServiceInterface,
	channelRouter *channels.Router,
) *CallbackDeps {
	return &CallbackDeps{
		Config:         cfg,
		SessionService: sessionService,
		ChannelRouter:  channelRouter,
	}
}

// NewSessionAdapterForCallback creates session adapter for callback handler
func NewSessionAdapterForCallback(svc *session.Service) SessionServiceInterface {
	return session.NewHandlerAdapter(svc)
}
