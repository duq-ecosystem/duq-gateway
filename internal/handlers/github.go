package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"duq-gateway/internal/config"
	"duq-gateway/internal/duq"
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

func GitHub(cfg *config.Config) http.HandlerFunc {
	client := duq.NewClient(cfg)
	secret := cfg.GitHubSecret

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

		if err := client.SendMessage(r.Context(), message); err != nil {
			log.Printf("Failed to send message: %v", err)
			http.Error(w, "Failed to send notification", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func formatGitHubMessage(eventType string, webhook GitHubWebhook) string {
	switch eventType {
	case "pull_request":
		if webhook.PullRequest == nil {
			return ""
		}
		pr := webhook.PullRequest
		switch webhook.Action {
		case "opened":
			return fmt.Sprintf("Новый PR в %s\n#%d: %s\nОт: %s\n%s",
				webhook.Repository.Name, pr.Number, pr.Title, pr.User.Login, pr.URL)
		case "closed":
			return fmt.Sprintf("PR закрыт в %s\n#%d: %s", webhook.Repository.Name, pr.Number, pr.Title)
		}

	case "issues":
		if webhook.Issue == nil {
			return ""
		}
		issue := webhook.Issue
		switch webhook.Action {
		case "opened":
			return fmt.Sprintf("Новый issue в %s\n#%d: %s\nОт: %s\n%s",
				webhook.Repository.Name, issue.Number, issue.Title, issue.User.Login, issue.URL)
		}

	case "push":
		return fmt.Sprintf("Push в %s от %s", webhook.Repository.Name, webhook.Sender.Login)
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
