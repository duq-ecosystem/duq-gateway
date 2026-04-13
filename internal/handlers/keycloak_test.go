package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"duq-gateway/internal/config"
)

// TestInitKeycloakClient tests that InitKeycloakClient initializes the client with correct timeout
func TestInitKeycloakClient(t *testing.T) {
	cfg := &config.Config{
		Timeouts: config.TimeoutsConfig{
			KeycloakTimeout: 15,
		},
	}

	InitKeycloakClient(cfg)

	client := getKeycloakClient()
	if client == nil {
		t.Fatal("Expected client to be initialized, got nil")
	}

	expectedTimeout := 15 * time.Second
	if client.Timeout != expectedTimeout {
		t.Errorf("Keycloak client timeout = %v, want %v", client.Timeout, expectedTimeout)
	}
}

// TestInitKeycloakClientZeroTimeout tests fallback when timeout is zero
func TestInitKeycloakClientZeroTimeout(t *testing.T) {
	cfg := &config.Config{
		Timeouts: config.TimeoutsConfig{
			KeycloakTimeout: 0, // zero value
		},
	}

	InitKeycloakClient(cfg)

	client := getKeycloakClient()
	if client == nil {
		t.Fatal("Expected client to be initialized, got nil")
	}

	// Should use fallback of 10 seconds
	expectedTimeout := 10 * time.Second
	if client.Timeout != expectedTimeout {
		t.Errorf("Keycloak client timeout = %v, want %v (fallback)", client.Timeout, expectedTimeout)
	}
}

// TestInitKeycloakClientCustomTimeout tests various timeout values
func TestInitKeycloakClientCustomTimeout(t *testing.T) {
	tests := []struct {
		name            string
		configTimeout   int
		expectedTimeout time.Duration
	}{
		{"5 seconds", 5, 5 * time.Second},
		{"30 seconds", 30, 30 * time.Second},
		{"60 seconds", 60, 60 * time.Second},
		{"zero uses fallback", 0, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Timeouts: config.TimeoutsConfig{
					KeycloakTimeout: tt.configTimeout,
				},
			}

			InitKeycloakClient(cfg)

			client := getKeycloakClient()
			if client.Timeout != tt.expectedTimeout {
				t.Errorf("timeout = %v, want %v", client.Timeout, tt.expectedTimeout)
			}
		})
	}
}

// TestGetKeycloakClientFallback tests that getKeycloakClient returns fallback if not initialized
func TestGetKeycloakClientFallback(t *testing.T) {
	// Reset the global client to nil
	keycloakHTTPClient = nil

	client := getKeycloakClient()
	if client == nil {
		t.Fatal("Expected fallback client, got nil")
	}

	// Fallback should have 10 second timeout
	expectedTimeout := 10 * time.Second
	if client.Timeout != expectedTimeout {
		t.Errorf("Fallback client timeout = %v, want %v", client.Timeout, expectedTimeout)
	}
}

// TestGetKeycloakClientReturnsInitialized tests that getKeycloakClient returns initialized client
func TestGetKeycloakClientReturnsInitialized(t *testing.T) {
	cfg := &config.Config{
		Timeouts: config.TimeoutsConfig{
			KeycloakTimeout: 25,
		},
	}

	InitKeycloakClient(cfg)

	// Get the client multiple times - should return same instance
	client1 := getKeycloakClient()
	client2 := getKeycloakClient()

	if client1 != client2 {
		t.Error("Expected same client instance, got different")
	}

	expectedTimeout := 25 * time.Second
	if client1.Timeout != expectedTimeout {
		t.Errorf("Client timeout = %v, want %v", client1.Timeout, expectedTimeout)
	}
}

// TestKeycloakLoginDisabled tests KeycloakLogin when Keycloak is disabled
func TestKeycloakLoginDisabled(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: false,
		},
	}

	handler := KeycloakLogin(cfg)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/login", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status 501 (Not Implemented), got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "not enabled") {
		t.Errorf("Expected 'not enabled' in response, got %q", rr.Body.String())
	}
}

// TestKeycloakLoginEnabled tests KeycloakLogin redirect when enabled
func TestKeycloakLoginEnabled(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled:  true,
			URL:      "http://keycloak:8180",
			Realm:    "duq",
			ClientID: "duq-gateway",
		},
		GatewayHost: "localhost:8082",
		TLS: config.TLSConfig{
			Enabled: false,
		},
	}

	handler := KeycloakLogin(cfg)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/login", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("Expected status 302 (Found), got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if location == "" {
		t.Error("Expected Location header to be set")
	}

	// Verify redirect URL contains expected components
	expectedParts := []string{
		"http://keycloak:8180",
		"/realms/duq/protocol/openid-connect/auth",
		"client_id=duq-gateway",
		"response_type=code",
		"scope=openid",
	}

	for _, part := range expectedParts {
		if !strings.Contains(location, part) {
			t.Errorf("Expected Location to contain %q, got %q", part, location)
		}
	}
}

// TestKeycloakRefreshDisabled tests KeycloakRefresh when Keycloak is disabled
func TestKeycloakRefreshDisabled(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: false,
		},
	}

	handler := KeycloakRefresh(cfg)

	req := httptest.NewRequest("POST", "/api/auth/keycloak/refresh", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status 501, got %d", rr.Code)
	}
}

// TestKeycloakRefreshMissingToken tests KeycloakRefresh with missing refresh_token
func TestKeycloakRefreshMissingToken(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
	}

	handler := KeycloakRefresh(cfg)

	// Empty body
	req := httptest.NewRequest("POST", "/api/auth/keycloak/refresh", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "Missing refresh_token") {
		t.Errorf("Expected 'Missing refresh_token' in response, got %q", rr.Body.String())
	}
}

// TestKeycloakLogoutDisabled tests KeycloakLogout when Keycloak is disabled
func TestKeycloakLogoutDisabled(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: false,
		},
	}

	handler := KeycloakLogout(cfg)

	req := httptest.NewRequest("POST", "/api/auth/keycloak/logout", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status 501, got %d", rr.Code)
	}
}

// TestKeycloakLogoutNoBody tests KeycloakLogout without body (redirect flow)
func TestKeycloakLogoutNoBody(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: true,
			URL:     "http://keycloak:8180",
			Realm:   "duq",
		},
		GatewayHost: "localhost:8082",
	}

	handler := KeycloakLogout(cfg)

	// Empty body triggers redirect
	req := httptest.NewRequest("POST", "/api/auth/keycloak/logout", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("Expected status 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "keycloak:8180") {
		t.Errorf("Expected redirect to keycloak, got %q", location)
	}
	if !strings.Contains(location, "logout") {
		t.Errorf("Expected logout in URL, got %q", location)
	}
}

// TestKeycloakUserInfoDisabled tests KeycloakUserInfo when Keycloak is disabled
func TestKeycloakUserInfoDisabled(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: false,
		},
	}

	handler := KeycloakUserInfo(cfg)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/userinfo", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status 501, got %d", rr.Code)
	}
}

// TestKeycloakUserInfoMissingAuth tests KeycloakUserInfo without Authorization header
func TestKeycloakUserInfoMissingAuth(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
	}

	handler := KeycloakUserInfo(cfg)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/userinfo", nil)
	// No Authorization header
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}
}

// TestKeycloakUserInfoInvalidAuth tests KeycloakUserInfo with invalid Authorization header
func TestKeycloakUserInfoInvalidAuth(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
	}

	handler := KeycloakUserInfo(cfg)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/userinfo", nil)
	req.Header.Set("Authorization", "Basic abc123") // Not Bearer
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}
}

// TestKeycloakCallbackDisabled tests KeycloakCallback when Keycloak is disabled
func TestKeycloakCallbackDisabled(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: false,
		},
	}

	handler := KeycloakCallback(cfg, nil)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/callback", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status 501, got %d", rr.Code)
	}
}

// TestKeycloakCallbackError tests KeycloakCallback with error from Keycloak
func TestKeycloakCallbackError(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
	}

	handler := KeycloakCallback(cfg, nil)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/callback?error=access_denied&error_description=User%20denied%20access", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "Authentication error") {
		t.Errorf("Expected 'Authentication error' in response, got %q", rr.Body.String())
	}
}

// TestKeycloakCallbackMissingCode tests KeycloakCallback without authorization code
func TestKeycloakCallbackMissingCode(t *testing.T) {
	cfg := &config.Config{
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
	}

	handler := KeycloakCallback(cfg, nil)

	req := httptest.NewRequest("GET", "/api/auth/keycloak/callback", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "Missing authorization code") {
		t.Errorf("Expected 'Missing authorization code' in response, got %q", rr.Body.String())
	}
}

// TestGetRedirectURI tests redirect URI generation
func TestGetRedirectURI(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectedURI string
	}{
		{
			name: "HTTP without TLS",
			cfg: &config.Config{
				GatewayHost: "localhost:8082",
				TLS: config.TLSConfig{
					Enabled: false,
				},
			},
			expectedURI: "http://localhost:8082/api/auth/keycloak/callback",
		},
		{
			name: "HTTPS with TLS",
			cfg: &config.Config{
				GatewayHost: "gateway.example.com",
				TLS: config.TLSConfig{
					Enabled: true,
				},
			},
			expectedURI: "https://gateway.example.com/api/auth/keycloak/callback",
		},
		{
			name: "Production domain",
			cfg: &config.Config{
				GatewayHost: "on-za-menya.online",
				TLS: config.TLSConfig{
					Enabled: true,
				},
			},
			expectedURI: "https://on-za-menya.online/api/auth/keycloak/callback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri := getRedirectURI(tt.cfg)
			if uri != tt.expectedURI {
				t.Errorf("getRedirectURI() = %q, want %q", uri, tt.expectedURI)
			}
		})
	}
}

// TestKeycloakTokenResponse tests KeycloakTokenResponse struct
func TestKeycloakTokenResponse(t *testing.T) {
	resp := &KeycloakTokenResponse{
		AccessToken:      "access-token-123",
		ExpiresIn:        300,
		RefreshExpiresIn: 1800,
		RefreshToken:     "refresh-token-456",
		TokenType:        "Bearer",
		IDToken:          "id-token-789",
		NotBeforePolicy:  0,
		SessionState:     "session-state-abc",
		Scope:            "openid profile email",
	}

	if resp.AccessToken != "access-token-123" {
		t.Errorf("AccessToken = %q, want %q", resp.AccessToken, "access-token-123")
	}
	if resp.ExpiresIn != 300 {
		t.Errorf("ExpiresIn = %d, want %d", resp.ExpiresIn, 300)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want %q", resp.TokenType, "Bearer")
	}
}

// TestKeycloakErrorResponse tests KeycloakErrorResponse struct
func TestKeycloakErrorResponse(t *testing.T) {
	resp := &KeycloakErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "Token is not active",
	}

	if resp.Error != "invalid_grant" {
		t.Errorf("Error = %q, want %q", resp.Error, "invalid_grant")
	}
	if resp.ErrorDescription != "Token is not active" {
		t.Errorf("ErrorDescription = %q, want %q", resp.ErrorDescription, "Token is not active")
	}
}
