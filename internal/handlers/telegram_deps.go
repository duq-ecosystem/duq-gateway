package handlers

import (
	"duq-gateway/internal/channels"
	"duq-gateway/internal/config"
	"duq-gateway/internal/credentials"
	"duq-gateway/internal/db"
	"duq-gateway/internal/queue"
	"duq-gateway/internal/registration"
	"duq-gateway/internal/session"
)

// TelegramDeps contains dependencies for the Telegram handler
type TelegramDeps struct {
	Config              *config.Config
	QueueClient         *queue.Client
	RBACService         RBACServiceInterface
	SessionService      SessionServiceInterface
	CredService         CredentialServiceInterface
	ChannelRouter       *channels.Router
	DBClient            *db.Client
	RegistrationService *registration.Service
}

// RBACServiceInterface for RBAC operations
type RBACServiceInterface interface {
	GetAllowedTools(userID int64) ([]string, error)
	EnsureUser(userID int64, username, firstName, lastName string) error
}

// SessionServiceInterface for conversation operations
type SessionServiceInterface interface {
	GetOrCreateConversationID(userID int64) (string, error)
	GetRecentMessagesSimple(conversationID string, limit int) ([]session.HistoryMessage, error)
	SaveMessageSimple(conversationID string, role, content string) error
}

// CredentialServiceInterface for user credentials operations
type CredentialServiceInterface interface {
	GetCredentials(userID int64, provider string) (*credentials.UserCredentials, error)
	SaveCredentials(creds *credentials.UserCredentials) error
}
