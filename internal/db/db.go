package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"duq-gateway/internal/config"

	_ "github.com/lib/pq"
)

// Client wraps PostgreSQL connection
type Client struct {
	db *sql.DB
}

// Config for database connection
type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// New creates a new DB client
func New(cfg Config) (*Client, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}

	log.Printf("[db] Connected to PostgreSQL: %s:%d/%s", cfg.Host, cfg.Port, cfg.Name)
	return &Client{db: db}, nil
}

// Close closes the database connection
func (c *Client) Close() error {
	return c.db.Close()
}

// DB returns the underlying sql.DB
func (c *Client) DB() *sql.DB {
	return c.db
}

// UserPreferences holds user-specific settings
type UserPreferences struct {
	Timezone          string
	PreferredLanguage string
}

// GetUserPreferencesByTelegramID returns user preferences by telegram_id
// Returns default values from config if user not found
func (c *Client) GetUserPreferencesByTelegramID(telegramID int64) *UserPreferences {
	var timezone, preferredLanguage string
	query := `SELECT COALESCE(timezone, $2), COALESCE(preferred_language, $3)
	          FROM users WHERE telegram_id = $1`
	err := c.db.QueryRow(query, telegramID, config.DefaultTimezone, config.DefaultPreferredLanguage).Scan(&timezone, &preferredLanguage)
	if err != nil {
		// User not found or error - return defaults
		return &UserPreferences{
			Timezone:          config.DefaultTimezone,
			PreferredLanguage: config.DefaultPreferredLanguage,
		}
	}
	return &UserPreferences{
		Timezone:          timezone,
		PreferredLanguage: preferredLanguage,
	}
}

// User represents a user record
type User struct {
	ID                int64
	TelegramID        int64
	Username          string
	FirstName         string
	LastName          string
	Role              string
	IsActive          bool
	Timezone          string
	PreferredLanguage string
}

// CheckUserExistsByTelegramID checks if a user with the given telegram_id exists
func (c *Client) CheckUserExistsByTelegramID(telegramID int64) bool {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = $1)`
	err := c.db.QueryRow(query, telegramID).Scan(&exists)
	if err != nil {
		log.Printf("[db] Error checking user existence: %v", err)
		return false
	}
	return exists
}

// GetUserByTelegramID returns full user info by telegram_id
func (c *Client) GetUserByTelegramID(telegramID int64) (*User, error) {
	query := `SELECT id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''),
	          COALESCE(last_name, ''), role, is_active,
	          COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
	          FROM users WHERE telegram_id = $1`

	var user User
	err := c.db.QueryRow(query, telegramID).Scan(
		&user.ID, &user.TelegramID, &user.Username, &user.FirstName,
		&user.LastName, &user.Role, &user.IsActive,
		&user.Timezone, &user.PreferredLanguage,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &user, nil
}

// CreateUserFromTelegram creates a new user from Telegram registration
// Returns the created user or error
func (c *Client) CreateUserFromTelegram(telegramID int64, username, firstName, lastName string) (*User, error) {
	query := `INSERT INTO users (telegram_id, username, first_name, last_name, role, is_active, timezone, preferred_language, created_at, updated_at)
	          VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), 'user', true, 'UTC', 'ru', NOW(), NOW())
	          ON CONFLICT (telegram_id) DO UPDATE SET
	            username = COALESCE(NULLIF(EXCLUDED.username, ''), users.username),
	            first_name = COALESCE(NULLIF(EXCLUDED.first_name, ''), users.first_name),
	            last_name = COALESCE(NULLIF(EXCLUDED.last_name, ''), users.last_name),
	            updated_at = NOW()
	          RETURNING id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''),
	                    COALESCE(last_name, ''), role, is_active, timezone, preferred_language`

	var user User
	err := c.db.QueryRow(query, telegramID, username, firstName, lastName).Scan(
		&user.ID, &user.TelegramID, &user.Username, &user.FirstName,
		&user.LastName, &user.Role, &user.IsActive,
		&user.Timezone, &user.PreferredLanguage,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("[db] Created/updated user from Telegram: id=%d, telegram_id=%d, username=%s",
		user.ID, user.TelegramID, user.Username)
	return &user, nil
}

// ===== EMAIL REGISTRATION METHODS =====

// UserWithEmail extends User with email field
type UserWithEmail struct {
	ID                int64
	Email             string
	TelegramID        *int64
	FirstName         string
	LastName          string
	Role              string
	IsActive          bool
	Timezone          string
	PreferredLanguage string
}

// CheckUserExistsByEmail checks if a user with the given email exists
func (c *Client) CheckUserExistsByEmail(email string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`
	err := c.db.QueryRow(query, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check email existence: %w", err)
	}
	return exists, nil
}

// GetUserByEmail returns user by email
func (c *Client) GetUserByEmail(email string) (*UserWithEmail, error) {
	query := `SELECT id, COALESCE(email, ''), telegram_id, COALESCE(first_name, ''),
	          COALESCE(last_name, ''), role, is_active,
	          COALESCE(timezone, 'UTC'), COALESCE(preferred_language, 'ru')
	          FROM users WHERE email = $1`

	var user UserWithEmail
	err := c.db.QueryRow(query, email).Scan(
		&user.ID, &user.Email, &user.TelegramID, &user.FirstName,
		&user.LastName, &user.Role, &user.IsActive,
		&user.Timezone, &user.PreferredLanguage,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return &user, nil
}

// CreateUserWithEmail creates a new user with email registration
// is_active=false until email is verified
func (c *Client) CreateUserWithEmail(email, passwordHash string, telegramID *int64, firstName, lastName string) (*UserWithEmail, error) {
	query := `INSERT INTO users (email, password_hash, telegram_id, first_name, last_name, role, is_active, timezone, preferred_language, created_at, updated_at)
	          VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), 'user', false, 'UTC', 'ru', NOW(), NOW())
	          RETURNING id, email, telegram_id, COALESCE(first_name, ''), COALESCE(last_name, ''),
	                    role, is_active, timezone, preferred_language`

	var user UserWithEmail
	err := c.db.QueryRow(query, email, passwordHash, telegramID, firstName, lastName).Scan(
		&user.ID, &user.Email, &user.TelegramID, &user.FirstName,
		&user.LastName, &user.Role, &user.IsActive,
		&user.Timezone, &user.PreferredLanguage,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("[db] Created user with email: id=%d, email=%s", user.ID, user.Email)
	return &user, nil
}

// ActivateUser sets is_active=true for a user
func (c *Client) ActivateUser(userID int64) error {
	query := `UPDATE users SET is_active = true, updated_at = NOW() WHERE id = $1`
	result, err := c.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to activate user: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	log.Printf("[db] Activated user: id=%d", userID)
	return nil
}

// ===== EMAIL VERIFICATION TOKEN METHODS =====

// CreateEmailVerificationToken creates a verification token for a user
// Note: email_verification_tokens table must exist (see migration 007)
func (c *Client) CreateEmailVerificationToken(userID int64, token string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl)
	query := `INSERT INTO email_verification_tokens (user_id, token, expires_at, created_at)
	          VALUES ($1, $2, $3, NOW())
	          ON CONFLICT (user_id) DO UPDATE SET token = $2, expires_at = $3, created_at = NOW()`

	_, err := c.db.Exec(query, userID, token, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to create verification token: %w", err)
	}

	log.Printf("[db] Created verification token for user %d, expires at %s", userID, expiresAt.Format(time.RFC3339))
	return nil
}

// ValidateEmailVerificationToken validates a token and returns the user ID
func (c *Client) ValidateEmailVerificationToken(token string) (int64, error) {
	var userID int64
	var expiresAt time.Time

	query := `SELECT user_id, expires_at FROM email_verification_tokens WHERE token = $1`
	err := c.db.QueryRow(query, token).Scan(&userID, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("token not found")
		}
		return 0, fmt.Errorf("failed to validate token: %w", err)
	}

	if time.Now().After(expiresAt) {
		return 0, fmt.Errorf("token expired")
	}

	return userID, nil
}

// DeleteEmailVerificationToken deletes a token after use
func (c *Client) DeleteEmailVerificationToken(token string) error {
	query := `DELETE FROM email_verification_tokens WHERE token = $1`
	_, err := c.db.Exec(query, token)
	if err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}
	return nil
}

// DeleteEmailVerificationTokenByUserID deletes all tokens for a user
func (c *Client) DeleteEmailVerificationTokenByUserID(userID int64) error {
	query := `DELETE FROM email_verification_tokens WHERE user_id = $1`
	_, err := c.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete tokens: %w", err)
	}
	return nil
}

