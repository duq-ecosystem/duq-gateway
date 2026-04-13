package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"duq-gateway/internal/config"
)

// TestDuqProxyClientTimeout verifies the proxy client has correct timeout
func TestDuqProxyClientTimeout(t *testing.T) {
	// Initialize the proxy client with default config
	cfg := &config.Config{
		Timeouts: config.TimeoutsConfig{
			ProxyTimeout: 60,
		},
	}
	InitProxyClient(cfg)

	if duqProxyClient == nil {
		t.Fatal("Expected proxy client to be initialized")
	}
	if duqProxyClient.Timeout != 60*time.Second {
		t.Errorf("Expected timeout 60s, got %v", duqProxyClient.Timeout)
	}
}

// TestInitProxyClient tests proxy client initialization
func TestInitProxyClient(t *testing.T) {
	cfg := &config.Config{
		Timeouts: config.TimeoutsConfig{
			ProxyTimeout: 30,
		},
	}

	InitProxyClient(cfg)

	if duqProxyClient == nil {
		t.Fatal("Expected proxy client to be initialized")
	}
	if duqProxyClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", duqProxyClient.Timeout)
	}
}

// TestInitProxyClientZeroTimeout tests fallback when timeout is zero
func TestInitProxyClientZeroTimeout(t *testing.T) {
	cfg := &config.Config{
		Timeouts: config.TimeoutsConfig{
			ProxyTimeout: 0, // zero triggers fallback
		},
	}

	InitProxyClient(cfg)

	if duqProxyClient == nil {
		t.Fatal("Expected proxy client to be initialized")
	}

	// Should use fallback of 60 seconds
	expectedTimeout := 60 * time.Second
	if duqProxyClient.Timeout != expectedTimeout {
		t.Errorf("Expected fallback timeout 60s, got %v", duqProxyClient.Timeout)
	}
}

// TestEnforceUserIDAccess tests RBAC enforcement
func TestEnforceUserIDAccess(t *testing.T) {
	tests := []struct {
		name          string
		requestedID   string
		contextUserID int64
		role          string
		expected      bool
	}{
		{"admin can access any user", "123", 456, "admin", true},
		{"root can access any user", "123", 456, "root", true},
		{"user can access own data", "123", 123, "user", true},
		{"user cannot access other data", "123", 456, "user", false},
		{"guest cannot access other data", "123", 456, "guest", false},
		{"invalid user_id returns false", "invalid", 123, "user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := enforceUserIDAccess(tt.requestedID, tt.contextUserID, tt.role)
			if result != tt.expected {
				t.Errorf("enforceUserIDAccess(%q, %d, %q) = %v, want %v",
					tt.requestedID, tt.contextUserID, tt.role, result, tt.expected)
			}
		})
	}
}

// TestIsAdmin tests admin role detection
func TestIsAdmin(t *testing.T) {
	tests := []struct {
		role     string
		expected bool
	}{
		{"root", true},
		{"admin", true},
		{"user", false},
		{"guest", false},
		{"public", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			if got := isAdmin(tt.role); got != tt.expected {
				t.Errorf("isAdmin(%q) = %v, want %v", tt.role, got, tt.expected)
			}
		})
	}
}

// TestProxyWorkflowsList tests the workflows list proxy with RBAC
func TestProxyWorkflowsList(t *testing.T) {
	// Create a mock Duq backend
	mockDuq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify user_id is set
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			t.Error("user_id should be set by proxy")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []string{},
		})
	}))
	defer mockDuq.Close()

	deps := &ProxyDeps{
		Config: &config.Config{
			DuqURL: mockDuq.URL,
		},
	}

	handler := ProxyWorkflowsList(deps)

	// Create request with user context
	req := httptest.NewRequest("GET", "/api/workflows", nil)
	ctx := context.WithValue(req.Context(), "user_id", int64(123))
	ctx = context.WithValue(ctx, "role", "user")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

// TestProxyWorkflowsListUnauthorized tests unauthorized access
func TestProxyWorkflowsListUnauthorized(t *testing.T) {
	deps := &ProxyDeps{
		Config: &config.Config{
			DuqURL: "http://localhost:8081",
		},
	}

	handler := ProxyWorkflowsList(deps)

	// Request without user context
	req := httptest.NewRequest("GET", "/api/workflows", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}
}

// TestProxyWorkflowCreate tests workflow creation with RBAC
func TestProxyWorkflowCreate(t *testing.T) {
	mockDuq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		userID := r.URL.Query().Get("user_id")
		if userID != "123" {
			t.Errorf("Expected user_id=123, got %s", userID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "workflow-1"})
	}))
	defer mockDuq.Close()

	deps := &ProxyDeps{
		Config: &config.Config{
			DuqURL: mockDuq.URL,
		},
	}

	handler := ProxyWorkflowCreate(deps)

	body := `{"name": "test workflow"}`
	req := httptest.NewRequest("POST", "/api/workflows", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), "user_id", int64(123))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", rr.Code)
	}
}

// TestProxyCortexStore tests cortex store with user_id injection
func TestProxyCortexStore(t *testing.T) {
	mockDuq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)

		// Check that user_id was injected
		if payload["user_id"] != "123" {
			t.Errorf("Expected user_id=123 in payload, got %v", payload["user_id"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer mockDuq.Close()

	deps := &ProxyDeps{
		Config: &config.Config{
			DuqURL: mockDuq.URL,
		},
	}

	handler := ProxyCortexStore(deps)

	body := `{"memory": "test memory"}`
	req := httptest.NewRequest("POST", "/api/cortex/store", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), "user_id", int64(123))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}
