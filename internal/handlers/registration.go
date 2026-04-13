package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
	"duq-gateway/internal/registration"
)

// RegistrationDeps contains dependencies for registration handlers
type RegistrationDeps struct {
	Config              *config.Config
	DBClient            *db.Client
	RegistrationService *registration.Service
}

// NewRegistrationDeps creates a new RegistrationDeps with the registration service
func NewRegistrationDeps(cfg *config.Config, dbClient *db.Client) *RegistrationDeps {
	return &RegistrationDeps{
		Config:              cfg,
		DBClient:            dbClient,
		RegistrationService: registration.NewService(cfg, dbClient),
	}
}

// UnifiedRegisterRequest is the unified request body for POST /api/auth/register
type UnifiedRegisterRequest struct {
	Method string `json:"method"` // "telegram", "email", "keycloak"

	// Telegram registration
	TelegramID *int64 `json:"telegram_id,omitempty"`
	Username   string `json:"username,omitempty"`

	// Email registration
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`

	// Keycloak registration
	AccessToken string `json:"access_token,omitempty"`

	// Common fields
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// UnifiedRegisterResponse is the unified response for registration
type UnifiedRegisterResponse struct {
	Success              bool               `json:"success"`
	Message              string             `json:"message"`
	UserID               int64              `json:"user_id,omitempty"`
	VerificationRequired bool               `json:"verification_required,omitempty"`
	User                 *registration.User `json:"user,omitempty"`
	Error                string             `json:"error,omitempty"`
	Code                 string             `json:"code,omitempty"`
	Details              map[string]string  `json:"details,omitempty"`
}

// Register creates a unified handler for POST /api/auth/register
func Register(deps *RegistrationDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UnifiedRegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, UnifiedRegisterResponse{
				Success: false,
				Error:   "Invalid request body",
				Code:    string(registration.ErrValidationError),
			})
			return
		}

		// Convert to registration.Request
		regReq := &registration.Request{
			Method:      registration.Method(req.Method),
			TelegramID:  req.TelegramID,
			Username:    req.Username,
			Email:       req.Email,
			Password:    req.Password,
			AccessToken: req.AccessToken,
			FirstName:   req.FirstName,
			LastName:    req.LastName,
		}

		// Call registration service
		resp, err := deps.RegistrationService.Register(r.Context(), regReq)
		if err != nil {
			handleRegistrationError(w, err, resp)
			return
		}

		// Success response
		statusCode := http.StatusCreated
		if resp.VerificationRequired {
			statusCode = http.StatusAccepted // 202 - action started but not complete
		}

		respondJSON(w, statusCode, UnifiedRegisterResponse{
			Success:              true,
			Message:              resp.Message,
			UserID:               resp.UserID,
			VerificationRequired: resp.VerificationRequired,
			User:                 resp.User,
		})
	}
}

// VerifyEmail creates a handler for GET /api/auth/verify-email
func VerifyEmail(deps *RegistrationDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")

		resp, err := deps.RegistrationService.VerifyEmail(token)
		if err != nil {
			if resp != nil {
				respondJSON(w, http.StatusBadRequest, UnifiedRegisterResponse{
					Success: false,
					Message: resp.Message,
					Code:    string(registration.ErrValidationError),
				})
				return
			}
			respondJSON(w, http.StatusInternalServerError, UnifiedRegisterResponse{
				Success: false,
				Error:   "Internal server error",
				Code:    string(registration.ErrInternalError),
			})
			return
		}

		respondJSON(w, http.StatusOK, UnifiedRegisterResponse{
			Success: true,
			Message: resp.Message,
			UserID:  resp.UserID,
		})
	}
}

// ResendVerification creates a handler for POST /api/auth/resend-verification
func ResendVerification(deps *RegistrationDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, UnifiedRegisterResponse{
				Success: false,
				Error:   "Invalid request body",
				Code:    string(registration.ErrValidationError),
			})
			return
		}

		resp, err := deps.RegistrationService.ResendVerification(req.Email)
		if err != nil {
			if validationErr, ok := err.(*registration.ValidationError); ok {
				respondJSON(w, http.StatusBadRequest, UnifiedRegisterResponse{
					Success: false,
					Message: validationErr.Message,
					Code:    string(registration.ErrValidationError),
				})
				return
			}
			respondJSON(w, http.StatusInternalServerError, UnifiedRegisterResponse{
				Success: false,
				Error:   "Internal server error",
				Code:    string(registration.ErrInternalError),
			})
			return
		}

		respondJSON(w, http.StatusOK, UnifiedRegisterResponse{
			Success: true,
			Message: resp.Message,
		})
	}
}

// handleRegistrationError handles registration errors and sends appropriate response
func handleRegistrationError(w http.ResponseWriter, err error, resp *registration.Response) {
	statusCode := http.StatusInternalServerError
	response := UnifiedRegisterResponse{
		Success: false,
		Error:   "Internal server error",
		Code:    string(registration.ErrInternalError),
	}

	switch e := err.(type) {
	case *registration.ValidationError:
		statusCode = http.StatusBadRequest
		response.Error = e.Message
		response.Code = string(registration.ErrValidationError)
		response.Details = e.Details
		if resp != nil {
			response.Message = resp.Message
		}

	case *registration.ConflictError:
		statusCode = http.StatusConflict
		response.Error = e.Message
		if e.Field == "email" {
			response.Code = string(registration.ErrEmailExists)
		} else if e.Field == "telegram_id" {
			response.Code = string(registration.ErrTelegramExists)
		}
		if resp != nil {
			response.Message = resp.Message
		}

	default:
		log.Printf("[registration] Internal error: %v", err)
	}

	respondJSON(w, statusCode, response)
}

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
