package registration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// Service orchestrates the registration process
type Service struct {
	cfg      *config.Config
	dbClient *db.Client

	// Strategies
	telegramStrategy *TelegramStrategy
	emailStrategy    *EmailStrategy
	keycloakStrategy *KeycloakStrategy
}

// NewService creates a new registration service
func NewService(cfg *config.Config, dbClient *db.Client) *Service {
	return &Service{
		cfg:              cfg,
		dbClient:         dbClient,
		telegramStrategy: NewTelegramStrategy(dbClient),
		emailStrategy:    NewEmailStrategy(cfg, dbClient),
		keycloakStrategy: NewKeycloakStrategy(cfg, dbClient),
	}
}

// Register handles unified registration
func (s *Service) Register(ctx context.Context, req *Request) (*Response, error) {
	// 1. Validate request
	if err := ValidateRequest(req); err != nil {
		if validationErr, ok := err.(*ValidationError); ok {
			return &Response{
				Success: false,
				Message: validationErr.Message,
			}, validationErr
		}
		return nil, err
	}

	// 2. Get strategy based on method
	strategy := s.getStrategy(req.Method)
	if strategy == nil {
		return &Response{
			Success: false,
			Message: "Invalid registration method",
		}, fmt.Errorf("unknown method: %s", req.Method)
	}

	// 3. Validate with strategy
	if err := strategy.Validate(req); err != nil {
		if validationErr, ok := err.(*ValidationError); ok {
			return &Response{
				Success: false,
				Message: validationErr.Message,
			}, validationErr
		}
		return nil, err
	}

	// 4. Register user
	user, err := strategy.Register(ctx, req)
	if err != nil {
		if conflictErr, ok := err.(*ConflictError); ok {
			return &Response{
				Success: false,
				Message: conflictErr.Message,
			}, conflictErr
		}
		return nil, err
	}

	// 5. Handle post-registration actions based on activation policy
	verificationRequired := false
	if strategy.GetActivationPolicy() == RequireEmailVerification {
		verificationRequired = true

		// Generate and save verification token
		token, err := generateVerificationToken()
		if err != nil {
			log.Printf("[registration] Warning: failed to generate verification token: %v", err)
		} else {
			// Save token (expires in 24 hours)
			err = s.dbClient.CreateEmailVerificationToken(user.ID, token, 24*time.Hour)
			if err != nil {
				log.Printf("[registration] Warning: failed to save verification token: %v", err)
			} else {
				// Send verification email
				err = s.sendVerificationEmail(user.Email, token)
				if err != nil {
					log.Printf("[registration] Warning: failed to send verification email: %v", err)
				}
			}
		}
	}

	// 6. Build response
	return &Response{
		Success:              true,
		Message:              s.getSuccessMessage(strategy.GetActivationPolicy()),
		UserID:               user.ID,
		VerificationRequired: verificationRequired,
		User:                 user,
	}, nil
}

// getStrategy returns the appropriate strategy for the given method
func (s *Service) getStrategy(method Method) Strategy {
	switch method {
	case MethodTelegram:
		return s.telegramStrategy
	case MethodEmail:
		return s.emailStrategy
	case MethodKeycloak:
		return s.keycloakStrategy
	default:
		return nil
	}
}

// getSuccessMessage returns the success message based on activation policy
func (s *Service) getSuccessMessage(policy ActivationPolicy) string {
	switch policy {
	case RequireEmailVerification:
		return "Registration successful. Please check your email to verify your account."
	default:
		return "Registration successful."
	}
}

// generateVerificationToken creates a secure random token
func generateVerificationToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// sendVerificationEmail sends email with verification link
func (s *Service) sendVerificationEmail(email, token string) error {
	verifyURL := fmt.Sprintf("https://%s/api/auth/verify-email?token=%s", s.cfg.TLS.Domain, token)

	subject := "Duq - Email Verification"
	body := fmt.Sprintf(`Welcome to Duq!

Please verify your email address by clicking the link below:

%s

This link will expire in 24 hours.

If you didn't create an account, please ignore this email.

Best regards,
Duq AI Assistant`, verifyURL)

	// Use gws CLI to send email
	cmd := exec.Command("gws", "gmail", "send",
		"--to", email,
		"--subject", subject,
		"--body", body,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[registration] Failed to send verification email: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	log.Printf("[registration] Verification email sent to %s", email)
	return nil
}

// VerifyEmail verifies an email using the verification token
func (s *Service) VerifyEmail(token string) (*Response, error) {
	if token == "" {
		return &Response{
			Success: false,
			Message: "Missing verification token",
		}, &ValidationError{Message: "missing token"}
	}

	// Find and validate token
	userID, err := s.dbClient.ValidateEmailVerificationToken(token)
	if err != nil {
		log.Printf("[registration] Invalid verification token: %v", err)
		return &Response{
			Success: false,
			Message: "Invalid or expired verification token",
		}, err
	}

	// Activate user
	err = s.dbClient.ActivateUser(userID)
	if err != nil {
		log.Printf("[registration] Error activating user %d: %v", userID, err)
		return nil, err
	}

	// Delete used token
	err = s.dbClient.DeleteEmailVerificationToken(token)
	if err != nil {
		log.Printf("[registration] Warning: failed to delete token: %v", err)
		// Don't fail the request
	}

	log.Printf("[registration] User verified: id=%d", userID)

	return &Response{
		Success: true,
		Message: "Email verified successfully. You can now log in.",
		UserID:  userID,
	}, nil
}

// ResendVerification resends the verification email
func (s *Service) ResendVerification(email string) (*Response, error) {
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return &Response{
			Success: false,
			Message: "Invalid email address",
		}, &ValidationError{Message: err.Error()}
	}

	// Find user by email
	user, err := s.dbClient.GetUserByEmail(email)
	if err != nil || user == nil {
		// Don't reveal if email exists
		return &Response{
			Success: true,
			Message: "If the email is registered, a verification link will be sent.",
		}, nil
	}

	// Check if already verified
	if user.IsActive {
		return &Response{
			Success: true,
			Message: "If the email is registered, a verification link will be sent.",
		}, nil
	}

	// Delete any existing tokens for this user
	s.dbClient.DeleteEmailVerificationTokenByUserID(user.ID)

	// Generate new verification token
	token, err := generateVerificationToken()
	if err != nil {
		log.Printf("[registration] Error generating token: %v", err)
		return nil, err
	}

	// Save verification token
	err = s.dbClient.CreateEmailVerificationToken(user.ID, token, 24*time.Hour)
	if err != nil {
		log.Printf("[registration] Error saving verification token: %v", err)
		return nil, err
	}

	// Send verification email
	err = s.sendVerificationEmail(email, token)
	if err != nil {
		log.Printf("[registration] Error sending verification email: %v", err)
		// Don't reveal error to user
	}

	log.Printf("[registration] Resent verification email to user %d", user.ID)

	return &Response{
		Success: true,
		Message: "If the email is registered, a verification link will be sent.",
	}, nil
}

// CheckUserExists checks if a user exists by telegram_id
func (s *Service) CheckUserExists(telegramID int64) bool {
	return s.dbClient.CheckUserExistsByTelegramID(telegramID)
}

// GetUserByTelegramID returns user by telegram_id
func (s *Service) GetUserByTelegramID(telegramID int64) (*User, error) {
	dbUser, err := s.dbClient.GetUserByTelegramID(telegramID)
	if err != nil {
		return nil, err
	}
	if dbUser == nil {
		return nil, nil
	}

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
