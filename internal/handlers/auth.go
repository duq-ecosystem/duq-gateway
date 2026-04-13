package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// QRGenerateRequest represents a QR code generation request
type QRGenerateRequest struct {
	TelegramID int64  `json:"telegram_id"`
	DeviceName string `json:"device_name,omitempty"`
}

// QRGenerateResponse represents a QR code generation response
type QRGenerateResponse struct {
	Code      string `json:"code"`
	QRData    string `json:"qr_data"`
	ExpiresIn int    `json:"expires_in"`
}

// QRVerifyRequest represents a QR code verification request
type QRVerifyRequest struct {
	Code string `json:"code"`
}

// QRVerifyResponse represents a QR code verification response
type QRVerifyResponse struct {
	Token      string    `json:"token"`
	TelegramID int64     `json:"telegram_id"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// QRGenerate handles QR code generation for mobile auth
func QRGenerate(cfg *config.Config, dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req QRGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.TelegramID == 0 {
			http.Error(w, "telegram_id is required", http.StatusBadRequest)
			return
		}

		// Generate QR code with configured TTL
		qrTTL := time.Duration(cfg.Timeouts.QRCodeTTLMin) * time.Minute
		if qrTTL == 0 {
			qrTTL = 5 * time.Minute // fallback default
		}
		qr, err := dbClient.GenerateQRCode(req.TelegramID, req.DeviceName, qrTTL)
		if err != nil {
			log.Printf("[auth] Failed to generate QR code: %v", err)
			http.Error(w, "Failed to generate code", http.StatusInternalServerError)
			return
		}

		log.Printf("[auth] QR code generated for telegram:%d, code=%s", req.TelegramID, qr.Code)

		response := QRGenerateResponse{
			Code:      qr.Code,
			QRData:    "duq://auth?code=" + qr.Code,
			ExpiresIn: int(qrTTL.Seconds()),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// QRVerifyError represents a structured error response
type QRVerifyError struct {
	Error        string `json:"error"`
	Code         string `json:"code"`
	NeedRegister bool   `json:"need_register,omitempty"`
}

// QRVerify handles QR code verification from mobile app
func QRVerify(cfg *config.Config, dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req QRVerifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.Code == "" {
			http.Error(w, "code is required", http.StatusBadRequest)
			return
		}

		// First, get the telegram_id from QR code to check user existence
		telegramID, err := dbClient.GetTelegramIDFromQRCode(req.Code)
		if err != nil {
			log.Printf("[auth] QR lookup failed: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(QRVerifyError{
				Error: "Invalid or expired QR code",
				Code:  "INVALID_CODE",
			})
			return
		}

		// Check if user exists in the users table
		userExists := dbClient.CheckUserExistsByTelegramID(telegramID)
		if !userExists {
			log.Printf("[auth] QR verify failed: user not registered (telegram_id=%d)", telegramID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(QRVerifyError{
				Error:        "User not registered. Please send /start to Duq Telegram bot first.",
				Code:         "USER_NOT_REGISTERED",
				NeedRegister: true,
			})
			return
		}

		// Session TTL from config or default 30 days
		sessionTTL := time.Duration(cfg.Voice.SessionTTLDays) * 24 * time.Hour
		if sessionTTL == 0 {
			sessionTTL = 30 * 24 * time.Hour
		}

		session, err := dbClient.VerifyQRCode(req.Code, sessionTTL)
		if err != nil {
			log.Printf("[auth] QR verification failed: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(QRVerifyError{
				Error: err.Error(),
				Code:  "VERIFICATION_FAILED",
			})
			return
		}

		log.Printf("[auth] Mobile session created for telegram:%d, token=%s...",
			session.TelegramID, session.Token[:20])

		response := QRVerifyResponse{
			Token:      session.Token,
			TelegramID: session.TelegramID,
			ExpiresAt:  session.ExpiresAt,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// JWT Authentication types and handlers

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token  string `json:"token"`
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
}

type Claims struct {
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Login handles username/password login and returns JWT token
func Login(cfg *config.Config, dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Fetch user from database
		var user struct {
			ID           int
			PasswordHash string
			Role         string
			IsActive     bool
		}

		query := `SELECT id, password_hash, role, is_active FROM users WHERE username = $1`
		err := dbClient.DB().QueryRow(query, req.Username).Scan(
			&user.ID, &user.PasswordHash, &user.Role, &user.IsActive,
		)
		if err == sql.ErrNoRows {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		if !user.IsActive {
			http.Error(w, "Account disabled", http.StatusForbidden)
			return
		}

		// Verify password
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Generate JWT token
		expirationTime := time.Now().Add(24 * time.Hour)
		claims := &Claims{
			UserID: user.ID,
			Role:   user.Role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expirationTime),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(cfg.JWTSecret))
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		log.Printf("[auth] User logged in: username=%s, user_id=%d, role=%s", req.Username, user.ID, user.Role)

		// Return token
		resp := LoginResponse{
			Token:  tokenString,
			UserID: user.ID,
			Role:   user.Role,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
