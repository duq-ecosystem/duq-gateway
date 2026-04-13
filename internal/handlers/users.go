package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"duq-gateway/internal/db"
)

type User struct {
	ID         int       `json:"id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"`
	TelegramID *int64    `json:"telegram_id"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateUserRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Role       string `json:"role"`
	TelegramID *int64 `json:"telegram_id"`
	IsActive   bool   `json:"is_active"`
}

type UpdateUserRequest struct {
	Username   *string `json:"username,omitempty"`
	Password   *string `json:"password,omitempty"`
	Role       *string `json:"role,omitempty"`
	TelegramID *int64  `json:"telegram_id,omitempty"`
	IsActive   *bool   `json:"is_active,omitempty"`
}

// Note: isAdmin helper function is defined in proxy.go to avoid duplication

// GET /api/users?search=&role=
func ListUsers(dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// RBAC: Only admin can list users
		currentUserID := r.Context().Value("user_id").(int64)
		currentRole := r.Context().Value("role").(string)

		if !isAdmin(currentRole) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		// Get query parameters
		search := r.URL.Query().Get("search")
		roleFilter := r.URL.Query().Get("role")

		// Build query
		query := `SELECT id, username, role, telegram_id, is_active, created_at FROM users WHERE 1=1`
		args := []interface{}{}
		argCount := 1

		if search != "" {
			query += ` AND username ILIKE $` + strconv.Itoa(argCount)
			args = append(args, "%"+search+"%")
			argCount++
		}

		if roleFilter != "" {
			query += ` AND role = $` + strconv.Itoa(argCount)
			args = append(args, roleFilter)
			argCount++
		}

		query += ` ORDER BY created_at DESC`

		rows, err := dbClient.DB().Query(query, args...)
		if err != nil {
			log.Printf("[users] List query error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		users := []User{}
		for rows.Next() {
			var u User
			err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.TelegramID, &u.IsActive, &u.CreatedAt)
			if err != nil {
				log.Printf("[users] Scan error: %v", err)
				continue
			}
			users = append(users, u)
		}

		log.Printf("[users] Listed %d users (requested by user_id=%d)", len(users), currentUserID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	}
}

// POST /api/users
func CreateUser(dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// RBAC: Only admin can create users
		currentRole := r.Context().Value("role").(string)
		if !isAdmin(currentRole) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		var req CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate
		if req.Username == "" || len(req.Username) < 3 {
			http.Error(w, "Username must be at least 3 characters", http.StatusBadRequest)
			return
		}

		if req.Password == "" || len(req.Password) < 8 {
			http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		if req.Role == "" {
			req.Role = "public" // Default role
		}

		// Hash password
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		// Insert user
		query := `
			INSERT INTO users (username, password_hash, role, telegram_id, is_active, created_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			RETURNING id, username, role, telegram_id, is_active, created_at
		`

		var user User
		err = dbClient.DB().QueryRow(query, req.Username, string(hashedPassword), req.Role, req.TelegramID, req.IsActive).Scan(
			&user.ID, &user.Username, &user.Role, &user.TelegramID, &user.IsActive, &user.CreatedAt,
		)

		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
				http.Error(w, "Username already exists", http.StatusConflict)
				return
			}
			log.Printf("[users] Create error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		log.Printf("[users] User created: id=%d, username=%s, role=%s", user.ID, user.Username, user.Role)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(user)
	}
}

// PUT /api/users/{id}
func UpdateUser(dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user ID from path
		userIDStr := r.PathValue("id")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid user ID", http.StatusBadRequest)
			return
		}

		// RBAC: Admin can update anyone, user can update only themselves
		currentUserID := r.Context().Value("user_id").(int64)
		currentRole := r.Context().Value("role").(string)

		if !isAdmin(currentRole) && currentUserID != userID {
			http.Error(w, "Forbidden: can only update own account", http.StatusForbidden)
			return
		}

		var req UpdateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Build UPDATE query dynamically
		updates := []string{}
		args := []interface{}{}
		argCount := 1

		if req.Username != nil {
			if len(*req.Username) < 3 {
				http.Error(w, "Username must be at least 3 characters", http.StatusBadRequest)
				return
			}
			updates = append(updates, "username = $"+strconv.Itoa(argCount))
			args = append(args, *req.Username)
			argCount++
		}

		if req.Password != nil {
			if len(*req.Password) < 8 {
				http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
				return
			}
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, "Failed to hash password", http.StatusInternalServerError)
				return
			}
			updates = append(updates, "password_hash = $"+strconv.Itoa(argCount))
			args = append(args, string(hashedPassword))
			argCount++
		}

		if req.Role != nil {
			// Only admin can change role
			if !isAdmin(currentRole) {
				http.Error(w, "Forbidden: cannot change role", http.StatusForbidden)
				return
			}
			updates = append(updates, "role = $"+strconv.Itoa(argCount))
			args = append(args, *req.Role)
			argCount++
		}

		if req.TelegramID != nil {
			updates = append(updates, "telegram_id = $"+strconv.Itoa(argCount))
			args = append(args, *req.TelegramID)
			argCount++
		}

		if req.IsActive != nil {
			// Only admin can change is_active
			if !isAdmin(currentRole) {
				http.Error(w, "Forbidden: cannot change account status", http.StatusForbidden)
				return
			}
			updates = append(updates, "is_active = $"+strconv.Itoa(argCount))
			args = append(args, *req.IsActive)
			argCount++
		}

		if len(updates) == 0 {
			http.Error(w, "No fields to update", http.StatusBadRequest)
			return
		}

		// Add user ID to args
		args = append(args, userID)

		query := `UPDATE users SET ` + strings.Join(updates, ", ") + ` WHERE id = $` + strconv.Itoa(argCount) + `
			RETURNING id, username, role, telegram_id, is_active, created_at`

		var user User
		err = dbClient.DB().QueryRow(query, args...).Scan(
			&user.ID, &user.Username, &user.Role, &user.TelegramID, &user.IsActive, &user.CreatedAt,
		)

		if err == sql.ErrNoRows {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
				http.Error(w, "Username already exists", http.StatusConflict)
				return
			}
			log.Printf("[users] Update error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		log.Printf("[users] User updated: id=%d, username=%s", user.ID, user.Username)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)
	}
}

// DELETE /api/users/{id}
func DeleteUser(dbClient *db.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// RBAC: Only admin can delete users
		currentRole := r.Context().Value("role").(string)
		if !isAdmin(currentRole) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		// Get user ID from path
		userIDStr := r.PathValue("id")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid user ID", http.StatusBadRequest)
			return
		}

		// Soft delete: set is_active = false
		query := `UPDATE users SET is_active = false WHERE id = $1`
		result, err := dbClient.DB().Exec(query, userID)
		if err != nil {
			log.Printf("[users] Delete error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		log.Printf("[users] User deleted (soft): id=%d", userID)

		w.WriteHeader(http.StatusNoContent)
	}
}
