package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"duq-gateway/internal/config"
	"duq-gateway/internal/duq"
)

type CustomWebhook struct {
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

func Custom(cfg *config.Config) http.HandlerFunc {
	client := duq.NewClient(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		var webhook CustomWebhook
		if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if webhook.Message == "" {
			http.Error(w, "Message is required", http.StatusBadRequest)
			return
		}

		log.Printf("Custom webhook from: %s", webhook.Source)

		if err := client.SendMessage(r.Context(), webhook.Message); err != nil {
			log.Printf("Failed to send message: %v", err)
			http.Error(w, "Failed to send notification", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
