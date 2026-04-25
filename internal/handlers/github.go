package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/queue"
)

type GitHubWebhook struct {
	Action      string           `json:"action"`
	Repository  GitHubRepository `json:"repository"`
	PullRequest *GitHubPR        `json:"pull_request,omitempty"`
	Issue       *GitHubIssue     `json:"issue,omitempty"`
	Sender      GitHubUser       `json:"sender"`
}

type GitHubRepository struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

type GitHubPR struct {
	Number int        `json:"number"`
	Title  string     `json:"title"`
	URL    string     `json:"html_url"`
	User   GitHubUser `json:"user"`
}

type GitHubIssue struct {
	Number int        `json:"number"`
	Title  string     `json:"title"`
	URL    string     `json:"html_url"`
	User   GitHubUser `json:"user"`
}

type GitHubUser struct {
	Login string `json:"login"`
}

// GitHubDeps contains dependencies for github handler
type GitHubDeps struct {
	Config      *config.Config
	QueueClient *queue.Client
}

func GitHub(deps *GitHubDeps) http.HandlerFunc {
	secret := deps.Config.GitHubSecret

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Verify signature if secret is configured
		if secret != "" {
			signature := r.Header.Get("X-Hub-Signature-256")
			if !verifyGitHubSignature(body, signature, secret) {
				http.Error(w, "Invalid signature", http.StatusUnauthorized)
				return
			}
		}

		eventType := r.Header.Get("X-GitHub-Event")

		var webhook GitHubWebhook
		if err := json.Unmarshal(body, &webhook); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		message := formatGitHubMessage(eventType, webhook)
		if message == "" {
			// Ignore unsupported events
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ignored"})
			return
		}

		log.Printf("GitHub webhook: %s/%s - %s", eventType, webhook.Action, webhook.Repository.Name)

		// Push to Redis queue instead of direct HTTP
		callbackURL := fmt.Sprintf("http://%s/api/duq/callback", deps.Config.GatewayHost)
		task := &queue.Task{
			UserID:      deps.Config.TelegramChatID,
			Type:        "notification",
			Priority:    50,
			CallbackURL: callbackURL,
			Payload: map[string]interface{}{
				"message":        message,
				"output_channel": "telegram",
				"source":         "github_webhook",
			},
			RequestMetadata: map[string]interface{}{
				"chat_id": deps.Config.TelegramChatID,
				"source":  "github",
			},
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		taskID, err := deps.QueueClient.Push(ctx, task)
		if err != nil {
			log.Printf("Failed to queue github notification: %v", err)
			http.Error(w, "Failed to queue notification", http.StatusInternalServerError)
			return
		}

		log.Printf("GitHub notification queued: task_id=%s", taskID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "task_id": taskID})
	}
}

func formatGitHubMessage(eventType string, webhook GitHubWebhook) string {
	prefix := "[GitHub уведомление] "
	suffix := "\n\nУведоми меня об этом в Telegram."

	switch eventType {
	case "pull_request":
		if webhook.PullRequest == nil {
			return ""
		}
		pr := webhook.PullRequest
		switch webhook.Action {
		case "opened":
			return fmt.Sprintf("%sНовый Pull Request в репозитории %s\n#%d: %s\nАвтор: %s\nСсылка: %s%s",
				prefix, webhook.Repository.Name, pr.Number, pr.Title, pr.User.Login, pr.URL, suffix)
		case "closed":
			return fmt.Sprintf("%sPull Request закрыт в репозитории %s\n#%d: %s%s",
				prefix, webhook.Repository.Name, pr.Number, pr.Title, suffix)
		case "merged":
			return fmt.Sprintf("%sPull Request смержен в репозитории %s\n#%d: %s%s",
				prefix, webhook.Repository.Name, pr.Number, pr.Title, suffix)
		}

	case "issues":
		if webhook.Issue == nil {
			return ""
		}
		issue := webhook.Issue
		switch webhook.Action {
		case "opened":
			return fmt.Sprintf("%sНовый Issue в репозитории %s\n#%d: %s\nАвтор: %s\nСсылка: %s%s",
				prefix, webhook.Repository.Name, issue.Number, issue.Title, issue.User.Login, issue.URL, suffix)
		case "closed":
			return fmt.Sprintf("%sIssue закрыт в репозитории %s\n#%d: %s%s",
				prefix, webhook.Repository.Name, issue.Number, issue.Title, suffix)
		}

	case "push":
		return fmt.Sprintf("%sНовый push в репозиторий %s от %s%s",
			prefix, webhook.Repository.Name, webhook.Sender.Login, suffix)
	}

	return ""
}

func verifyGitHubSignature(body []byte, signature, secret string) bool {
	if signature == "" {
		return false
	}

	// Signature format: sha256=<hex>
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}
