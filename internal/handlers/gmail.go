package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/queue"
)

type GmailWebhook struct {
	Type    string   `json:"type"` // "new_email", "important"
	From    string   `json:"from"`
	Subject string   `json:"subject"`
	Snippet string   `json:"snippet,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

// GmailDeps contains dependencies for gmail handler
type GmailDeps struct {
	Config      *config.Config
	QueueClient *queue.Client
}

func Gmail(deps *GmailDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var webhook GmailWebhook
		if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		message := formatGmailMessage(webhook)
		if message == "" {
			http.Error(w, "Unknown webhook type", http.StatusBadRequest)
			return
		}

		log.Printf("Gmail webhook: %s - %s", webhook.Type, webhook.Subject)

		// Push to Redis queue instead of direct HTTP
		callbackURL := fmt.Sprintf("http://%s/api/duq/callback", deps.Config.GatewayHost)
		task := &queue.Task{
			UserID:      deps.Config.TelegramChatID,
			Type:        "notification",
			Priority:    60,
			CallbackURL: callbackURL,
			Payload: map[string]interface{}{
				"message":        message,
				"output_channel": "telegram",
				"source":         "gmail_webhook",
			},
			RequestMetadata: map[string]interface{}{
				"chat_id": deps.Config.TelegramChatID,
				"source":  "gmail",
			},
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID, err := deps.QueueClient.Push(ctx, task)
		if err != nil {
			log.Printf("Failed to queue gmail notification: %v", err)
			http.Error(w, "Failed to queue notification", http.StatusInternalServerError)
			return
		}

		log.Printf("Gmail notification queued: task_id=%s", taskID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "task_id": taskID})
	}
}

func formatGmailMessage(webhook GmailWebhook) string {
	prefix := "[Gmail уведомление] "
	suffix := "\n\nЭто автоматическое уведомление."

	switch webhook.Type {
	case "new_email", "important":
		msg := fmt.Sprintf("%sНовое письмо от %s\nТема: %s", prefix, webhook.From, webhook.Subject)
		if webhook.Snippet != "" {
			msg += fmt.Sprintf("\n\n%s...", webhook.Snippet)
		}
		return msg + suffix
	default:
		return ""
	}
}
