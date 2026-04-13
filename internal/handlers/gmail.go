package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"duq-gateway/internal/config"
	"duq-gateway/internal/duq"
)

type GmailWebhook struct {
	Type    string   `json:"type"` // "new_email", "important"
	From    string   `json:"from"`
	Subject string   `json:"subject"`
	Snippet string   `json:"snippet,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

func Gmail(cfg *config.Config) http.HandlerFunc {
	client := duq.NewClient(cfg)

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

		if err := client.SendMessage(r.Context(), message); err != nil {
			log.Printf("Failed to send message: %v", err)
			http.Error(w, "Failed to send notification", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func formatGmailMessage(webhook GmailWebhook) string {
	switch webhook.Type {
	case "new_email", "important":
		msg := fmt.Sprintf("Новое письмо от %s\nТема: %s", webhook.From, webhook.Subject)
		if webhook.Snippet != "" {
			msg += fmt.Sprintf("\n\n%s...", webhook.Snippet)
		}
		return msg
	default:
		return ""
	}
}
