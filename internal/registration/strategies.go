package registration

import (
	"context"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// Strategy defines the interface for registration strategies
type Strategy interface {
	// Validate validates the registration request
	Validate(req *Request) error

	// Register creates a new user
	Register(ctx context.Context, req *Request) (*User, error)

	// GetActivationPolicy returns the activation policy for this strategy
	GetActivationPolicy() ActivationPolicy
}

// TelegramStrategy handles Telegram-based registration
type TelegramStrategy struct {
	dbClient *db.Client
}

// NewTelegramStrategy creates a new Telegram strategy
func NewTelegramStrategy(dbClient *db.Client) *TelegramStrategy {
	return &TelegramStrategy{dbClient: dbClient}
}

func (s *TelegramStrategy) Validate(req *Request) error {
	return ValidateTelegramRequest(req)
}

func (s *TelegramStrategy) Register(ctx context.Context, req *Request) (*User, error) {
	// Check if user already exists
	if s.dbClient.CheckUserExistsByTelegramID(*req.TelegramID) {
		// Get existing user
		dbUser, err := s.dbClient.GetUserByTelegramID(*req.TelegramID)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing user: %w", err)
		}
		if dbUser != nil {
			log.Printf("[registration] Telegram user already exists: telegram_id=%d", *req.TelegramID)
			return &User{
				ID:                dbUser.ID,
				TelegramID:        &dbUser.TelegramID,
				Username:          dbUser.Username,
				FirstName:         dbUser.FirstName,
				LastName:          dbUser.LastName,
				Role:              dbUser.Role,
				IsActive:          dbUser.IsActive,
				Timezone:          dbUser.Timezone,
				PreferredLanguage: dbUser.PreferredLanguage,
			}, nil
		}
	}

	// Create new user
	dbUser, err := s.dbClient.CreateUserFromTelegram(*req.TelegramID, req.Username, req.FirstName, req.LastName)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram user: %w", err)
	}

	log.Printf("[registration] Created Telegram user: id=%d, telegram_id=%d", dbUser.ID, dbUser.TelegramID)

	return &User{
		ID:                dbUser.ID,
		TelegramID:        &dbUser.TelegramID,
		Username:          dbUser.Username,
		FirstName:         dbUser.FirstName,
		LastName:          dbUser.LastName,
		Role:              dbUser.Role,
		IsActive:          dbUser.IsActive,
		Timezone:          dbUser.Timezone,
		PreferredLanguage: dbUser.PreferredLanguage,
	}, nil
}

func (s *TelegramStrategy) GetActivationPolicy() ActivationPolicy {
	return ActivateImmediately
}

// EmailStrategy handles email-based registration
type EmailStrategy struct {
	cfg      *config.Config
	dbClient *db.Client
}

// NewEmailStrategy creates a new Email strategy
func NewEmailStrategy(cfg *config.Config, dbClient *db.Client) *EmailStrategy {
	return &EmailStrategy{cfg: cfg, dbClient: dbClient}
}

func (s *EmailStrategy) Validate(req *Request) error {
	return ValidateEmailRequest(req)
}

func (s *EmailStrategy) Register(ctx context.Context, req *Request) (*User, error) {
	// Normalize email
	email := NormalizeEmail(req.Email)

	// Check if email already exists
	exists, err := s.dbClient.CheckUserExistsByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("failed to check email existence: %w", err)
	}
	if exists {
		return nil, &ConflictError{Field: "email", Message: "email already registered"}
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user with is_active=false
	dbUser, err := s.dbClient.CreateUserWithEmail(email, string(hashedPassword), req.TelegramID, req.FirstName, req.LastName)
	if err != nil {
		return nil, fmt.Errorf("failed to create email user: %w", err)
	}

	log.Printf("[registration] Created email user: id=%d, email=%s (requires verification)", dbUser.ID, dbUser.Email)

	return &User{
		ID:                dbUser.ID,
		Email:             dbUser.Email,
		TelegramID:        dbUser.TelegramID,
		FirstName:         dbUser.FirstName,
		LastName:          dbUser.LastName,
		Role:              dbUser.Role,
		IsActive:          dbUser.IsActive,
		Timezone:          dbUser.Timezone,
		PreferredLanguage: dbUser.PreferredLanguage,
	}, nil
}

func (s *EmailStrategy) GetActivationPolicy() ActivationPolicy {
	return RequireEmailVerification
}

// KeycloakStrategy handles Keycloak-based registration
// Note: Keycloak registration is typically handled during SSO callback,
// this strategy is for explicit registration calls if needed
type KeycloakStrategy struct {
	cfg      *config.Config
	dbClient *db.Client
}

// NewKeycloakStrategy creates a new Keycloak strategy
func NewKeycloakStrategy(cfg *config.Config, dbClient *db.Client) *KeycloakStrategy {
	return &KeycloakStrategy{cfg: cfg, dbClient: dbClient}
}

func (s *KeycloakStrategy) Validate(req *Request) error {
	return ValidateKeycloakRequest(req)
}

func (s *KeycloakStrategy) Register(ctx context.Context, req *Request) (*User, error) {
	// Keycloak registration is typically handled in the Keycloak callback middleware
	// This method is a placeholder for explicit API-based Keycloak registration
	return nil, fmt.Errorf("keycloak registration should be done via SSO callback")
}

func (s *KeycloakStrategy) GetActivationPolicy() ActivationPolicy {
	return ActivateImmediately
}

// ConflictError represents a conflict error (e.g., email already exists)
type ConflictError struct {
	Field   string
	Message string
}

func (e *ConflictError) Error() string {
	return e.Message
}
