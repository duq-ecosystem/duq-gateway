package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/duq"
)

type CalendarEvent struct {
	Title     string `json:"title"`
	StartTime string `json:"start_time"` // RFC3339
	EndTime   string `json:"end_time"`   // RFC3339
	Location  string `json:"location,omitempty"`
	MeetLink  string `json:"meet_link,omitempty"`
	EventID   string `json:"event_id,omitempty"`
}

type CalendarWebhook struct {
	Type    string        `json:"type"` // "reminder", "created", "updated", "cancelled"
	Event   CalendarEvent `json:"event"`
	Minutes int           `json:"minutes_before,omitempty"` // for reminders
}

func Calendar(cfg *config.Config) http.HandlerFunc {
	client := duq.NewClient(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		var webhook CalendarWebhook
		if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		message := formatCalendarMessage(webhook)
		if message == "" {
			http.Error(w, "Unknown webhook type", http.StatusBadRequest)
			return
		}

		log.Printf("Calendar webhook: %s - %s", webhook.Type, webhook.Event.Title)

		if err := client.SendMessage(r.Context(), message); err != nil {
			log.Printf("Failed to send message: %v", err)
			http.Error(w, "Failed to send notification", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func formatCalendarMessage(webhook CalendarWebhook) string {
	event := webhook.Event

	// Parse start time
	startTime, err := time.Parse(time.RFC3339, event.StartTime)
	if err != nil {
		startTime = time.Now()
	}
	timeStr := startTime.Format("15:04")

	switch webhook.Type {
	case "reminder":
		msg := fmt.Sprintf("Напоминание: %s в %s", event.Title, timeStr)
		if event.MeetLink != "" {
			msg += fmt.Sprintf("\nСсылка: %s", event.MeetLink)
		}
		if event.Location != "" {
			msg += fmt.Sprintf("\nМесто: %s", event.Location)
		}
		return msg

	case "created":
		return fmt.Sprintf("Новое событие: %s в %s", event.Title, timeStr)

	case "updated":
		return fmt.Sprintf("Событие изменено: %s, новое время: %s", event.Title, timeStr)

	case "cancelled":
		return fmt.Sprintf("Событие отменено: %s", event.Title)

	default:
		return ""
	}
}
