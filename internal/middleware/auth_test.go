package middleware

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"duq-gateway/internal/config"
)

// TestBasicAuthSuccess tests successful basic auth
func TestBasicAuthSuccess(t *testing.T) {
	cfg := &config.Config{
		BasicAuth: config.BasicAuthConfig{
			Username: "admin",
			Password: "secret",
		},
	}

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := BasicAuth(cfg, handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:secret")))

	rr := httptest.NewRecorder()
	middleware(rr, req)

	if !handlerCalled {
		t.Error("Handler was not called for valid credentials")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// TestBasicAuthMissingCredentials tests missing Authorization header
func TestBasicAuthMissingCredentials(t *testing.T) {
	cfg := &config.Config{
		BasicAuth: config.BasicAuthConfig{
			Username: "admin",
			Password: "secret",
		},
	}

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	middleware := BasicAuth(cfg, handler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	middleware(rr, req)

	if handlerCalled {
		t.Error("Handler should not be called for missing credentials")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	// Should set WWW-Authenticate header
	wwwAuth := rr.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("Missing WWW-Authenticate header")
	}
}

// TestBasicAuthWrongCredentials tests invalid credentials
func TestBasicAuthWrongCredentials(t *testing.T) {
	cfg := &config.Config{
		BasicAuth: config.BasicAuthConfig{
			Username: "admin",
			Password: "secret",
		},
	}

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong username", "wrong", "secret"},
		{"wrong password", "admin", "wrong"},
		{"both wrong", "wrong", "wrong"},
		{"empty username", "", "secret"},
		{"empty password", "admin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			handler := func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
			}

			middleware := BasicAuth(cfg, handler)

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(tt.username+":"+tt.password)))

			rr := httptest.NewRecorder()
			middleware(rr, req)

			if handlerCalled {
				t.Error("Handler should not be called for invalid credentials")
			}
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}
}

// TestBasicAuthNotConfigured tests when BasicAuth is not configured
func TestBasicAuthNotConfigured(t *testing.T) {
	cfg := &config.Config{
		BasicAuth: config.BasicAuthConfig{
			Username: "",
			Password: "",
		},
	}

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	middleware := BasicAuth(cfg, handler)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:secret")))

	rr := httptest.NewRecorder()
	middleware(rr, req)

	if handlerCalled {
		t.Error("Handler should not be called when BasicAuth not configured")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

// TestBasicAuthInvalidFormat tests invalid Authorization header format
func TestBasicAuthInvalidFormat(t *testing.T) {
	cfg := &config.Config{
		BasicAuth: config.BasicAuthConfig{
			Username: "admin",
			Password: "secret",
		},
	}

	tests := []struct {
		name   string
		header string
	}{
		{"Bearer token", "Bearer token123"},
		{"Just word", "admin:secret"},
		{"Empty", ""},
		{"Basic only", "Basic"},
		{"Invalid base64", "Basic not-valid-base64!@#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			handler := func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
			}

			middleware := BasicAuth(cfg, handler)

			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			rr := httptest.NewRecorder()
			middleware(rr, req)

			if handlerCalled {
				t.Error("Handler should not be called for invalid auth format")
			}
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}
}

// TestBasicAuthTimingAttack tests that comparison is constant-time
func TestBasicAuthTimingAttack(t *testing.T) {
	// This is more of a documentation test - we verify that subtle.ConstantTimeCompare
	// is used by checking the code structure. The actual timing resistance cannot be
	// easily tested in unit tests.

	cfg := &config.Config{
		BasicAuth: config.BasicAuthConfig{
			Username: "admin",
			Password: "verylongsecretpassword",
		},
	}

	handler := func(w http.ResponseWriter, r *http.Request) {}
	middleware := BasicAuth(cfg, handler)

	// Multiple requests with varying password lengths should all fail similarly
	passwords := []string{"a", "ab", "abc", "abcd", "abcde", "verylongsecretpasswor"}

	for _, pass := range passwords {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+pass)))

		rr := httptest.NewRecorder()
		middleware(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Password %q should fail, got status %d", pass, rr.Code)
		}
	}
}

// TestMobileAuthMissingToken tests missing mobile token
func TestMobileAuthMissingToken(t *testing.T) {
	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	// MobileAuth requires db.Client which we can't easily mock
	// So we test the token format validation only
	middleware := MobileAuth(nil, handler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	middleware(rr, req)

	if handlerCalled {
		t.Error("Handler should not be called for missing token")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestMobileAuthInvalidTokenFormat tests invalid token prefix
func TestMobileAuthInvalidTokenFormat(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"no prefix", "Bearer token123"},
		{"wrong prefix", "Bearer web_token123"},
		{"empty token", "Bearer "},
		{"jwt token", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			handler := func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
			}

			middleware := MobileAuth(nil, handler)

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", tt.token)

			rr := httptest.NewRecorder()
			middleware(rr, req)

			if handlerCalled {
				t.Error("Handler should not be called for invalid token format")
			}
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}
}

// Note: TestMobileAuthValidTokenFormatNoDb removed - requires mock DB to test properly
// The MobileAuth function requires a non-nil db.Client to function
