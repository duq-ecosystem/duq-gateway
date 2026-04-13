package credentials

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// UserCredentials holds OAuth tokens for a user
type UserCredentials struct {
	ID           int64
	UserID       int64
	Provider     string
	Email        string // User's email from OAuth provider
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	Scopes       []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Service handles user credential operations
type Service struct {
	db *sql.DB
}

// NewService creates a new credentials service
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// DB returns the database connection for direct queries
func (s *Service) DB() *sql.DB {
	return s.db
}

// GetCredentials retrieves credentials for a user and provider
func (s *Service) GetCredentials(userID int64, provider string) (*UserCredentials, error) {
	query := `
		SELECT id, user_id, provider, COALESCE(email, ''), access_token, refresh_token,
		       token_type, expires_at, scopes, created_at, updated_at
		FROM user_credentials
		WHERE user_id = $1 AND provider = $2
	`

	var creds UserCredentials
	var expiresAt sql.NullTime
	var refreshToken sql.NullString
	var accessToken sql.NullString
	var scopes []string

	err := s.db.QueryRow(query, userID, provider).Scan(
		&creds.ID, &creds.UserID, &creds.Provider, &creds.Email,
		&accessToken, &refreshToken,
		&creds.TokenType, &expiresAt, pq.Array(&scopes),
		&creds.CreatedAt, &creds.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	if accessToken.Valid {
		creds.AccessToken = accessToken.String
	}
	if refreshToken.Valid {
		creds.RefreshToken = refreshToken.String
	}
	if expiresAt.Valid {
		creds.ExpiresAt = expiresAt.Time
	}
	creds.Scopes = scopes

	return &creds, nil
}

// SaveCredentials saves or updates credentials for a user
func (s *Service) SaveCredentials(creds *UserCredentials) error {
	query := `
		INSERT INTO user_credentials (user_id, provider, email, access_token, refresh_token, token_type, expires_at, scopes, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (user_id, provider)
		DO UPDATE SET
			email = COALESCE(NULLIF(EXCLUDED.email, ''), user_credentials.email),
			access_token = EXCLUDED.access_token,
			refresh_token = COALESCE(EXCLUDED.refresh_token, user_credentials.refresh_token),
			token_type = EXCLUDED.token_type,
			expires_at = EXCLUDED.expires_at,
			scopes = EXCLUDED.scopes,
			updated_at = NOW()
		RETURNING id
	`

	var expiresAt interface{}
	if !creds.ExpiresAt.IsZero() {
		expiresAt = creds.ExpiresAt
	}

	var refreshToken interface{}
	if creds.RefreshToken != "" {
		refreshToken = creds.RefreshToken
	}

	var email interface{}
	if creds.Email != "" {
		email = creds.Email
	}

	err := s.db.QueryRow(
		query,
		creds.UserID, creds.Provider, email,
		creds.AccessToken, refreshToken,
		creds.TokenType, expiresAt, pq.Array(creds.Scopes),
	).Scan(&creds.ID)

	if err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	return nil
}

// DeleteCredentials removes credentials for a user and provider
func (s *Service) DeleteCredentials(userID int64, provider string) error {
	query := `DELETE FROM user_credentials WHERE user_id = $1 AND provider = $2`
	_, err := s.db.Exec(query, userID, provider)
	if err != nil {
		return fmt.Errorf("failed to delete credentials: %w", err)
	}
	return nil
}

// IsExpired checks if credentials are expired (with 5 min buffer)
func (c *UserCredentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

// ToMap converts credentials to a map for sending to Duq
func (c *UserCredentials) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"access_token":  c.AccessToken,
		"refresh_token": c.RefreshToken,
		"token_type":    c.TokenType,
		"expires_at":    c.ExpiresAt.Unix(),
	}
}
