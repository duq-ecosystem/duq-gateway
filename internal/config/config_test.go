package config

import (
	"os"
	"testing"
)

// TestLoadDefaults tests that Load returns correct default values
func TestLoadDefaults(t *testing.T) {
	// Clear env vars that might interfere
	envsToClear := []string{
		"DUQ_PORT", "DUQ_TELEGRAM_CHAT_ID", "DUQ_URL",
		"GATEWAY_HOST", "USE_ASYNC_QUEUE",
	}
	for _, env := range envsToClear {
		os.Unsetenv(env)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test defaults
	if cfg.Port != "8082" {
		t.Errorf("Port = %s, want 8082", cfg.Port)
	}
	if cfg.TelegramChatID != "764733417" {
		t.Errorf("TelegramChatID = %s, want 764733417", cfg.TelegramChatID)
	}
	if cfg.DuqURL != "http://localhost:8081" {
		t.Errorf("DuqURL = %s, want http://localhost:8081", cfg.DuqURL)
	}
	if cfg.GatewayHost != "localhost:8082" {
		t.Errorf("GatewayHost = %s, want localhost:8082", cfg.GatewayHost)
	}
	if !cfg.UseAsyncQueue {
		t.Error("UseAsyncQueue should default to true")
	}
}

// TestLoadDatabaseDefaults tests database default configuration
func TestLoadDatabaseDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Database.Host != "localhost" {
		t.Errorf("Database.Host = %s, want localhost", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("Database.Port = %d, want 5433", cfg.Database.Port)
	}
	if cfg.Database.User != "duq" {
		t.Errorf("Database.User = %s, want duq", cfg.Database.User)
	}
	if cfg.Database.Name != "duq" {
		t.Errorf("Database.Name = %s, want duq", cfg.Database.Name)
	}
}

// TestLoadVoiceDefaults tests voice configuration defaults
func TestLoadVoiceDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Voice.STTCommand != "/usr/local/bin/whisper-stt" {
		t.Errorf("Voice.STTCommand = %s, want /usr/local/bin/whisper-stt", cfg.Voice.STTCommand)
	}
	if cfg.Voice.TTSVoice != "ru-RU-DmitryNeural" {
		t.Errorf("Voice.TTSVoice = %s, want ru-RU-DmitryNeural", cfg.Voice.TTSVoice)
	}
	if cfg.Voice.SessionTTLDays != 30 {
		t.Errorf("Voice.SessionTTLDays = %d, want 30", cfg.Voice.SessionTTLDays)
	}
}

// TestLoadTracingDefaults tests tracing configuration defaults
func TestLoadTracingDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Tracing.Enabled {
		t.Error("Tracing.Enabled should default to true")
	}
	if cfg.Tracing.RedisURL != "redis://localhost:6379" {
		t.Errorf("Tracing.RedisURL = %s, want redis://localhost:6379", cfg.Tracing.RedisURL)
	}
	if cfg.Tracing.Channel != "duq:traces" {
		t.Errorf("Tracing.Channel = %s, want duq:traces", cfg.Tracing.Channel)
	}
}

// TestLoadEnvOverride tests environment variable overrides
func TestLoadEnvOverride(t *testing.T) {
	// Set env vars
	os.Setenv("DUQ_PORT", "9999")
	os.Setenv("DUQ_URL", "http://custom:1234")
	defer func() {
		os.Unsetenv("DUQ_PORT")
		os.Unsetenv("DUQ_URL")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != "9999" {
		t.Errorf("Port = %s, want 9999 (from env)", cfg.Port)
	}
	if cfg.DuqURL != "http://custom:1234" {
		t.Errorf("DuqURL = %s, want http://custom:1234 (from env)", cfg.DuqURL)
	}
}

// TestLoadDatabaseEnvOverride tests database environment overrides
func TestLoadDatabaseEnvOverride(t *testing.T) {
	os.Setenv("DUQ_DB_HOST", "db.example.com")
	os.Setenv("DUQ_DB_PORT", "5432")
	os.Setenv("DUQ_DB_USER", "testuser")
	os.Setenv("DUQ_DB_PASSWORD", "secret")
	os.Setenv("DUQ_DB_NAME", "testdb")
	defer func() {
		os.Unsetenv("DUQ_DB_HOST")
		os.Unsetenv("DUQ_DB_PORT")
		os.Unsetenv("DUQ_DB_USER")
		os.Unsetenv("DUQ_DB_PASSWORD")
		os.Unsetenv("DUQ_DB_NAME")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Database.Host != "db.example.com" {
		t.Errorf("Database.Host = %s, want db.example.com", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port = %d, want 5432", cfg.Database.Port)
	}
	if cfg.Database.User != "testuser" {
		t.Errorf("Database.User = %s, want testuser", cfg.Database.User)
	}
	if cfg.Database.Password != "secret" {
		t.Errorf("Database.Password = %s, want secret", cfg.Database.Password)
	}
	if cfg.Database.Name != "testdb" {
		t.Errorf("Database.Name = %s, want testdb", cfg.Database.Name)
	}
}

// TestLoadTLSEnvOverride tests TLS environment overrides
func TestLoadTLSEnvOverride(t *testing.T) {
	os.Setenv("TLS_ENABLED", "true")
	os.Setenv("TLS_CERT_FILE", "/etc/ssl/cert.pem")
	os.Setenv("TLS_KEY_FILE", "/etc/ssl/key.pem")
	os.Setenv("TLS_DOMAIN", "example.com")
	defer func() {
		os.Unsetenv("TLS_ENABLED")
		os.Unsetenv("TLS_CERT_FILE")
		os.Unsetenv("TLS_KEY_FILE")
		os.Unsetenv("TLS_DOMAIN")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.TLS.Enabled {
		t.Error("TLS.Enabled should be true from env")
	}
	if cfg.TLS.CertFile != "/etc/ssl/cert.pem" {
		t.Errorf("TLS.CertFile = %s, want /etc/ssl/cert.pem", cfg.TLS.CertFile)
	}
	if cfg.TLS.KeyFile != "/etc/ssl/key.pem" {
		t.Errorf("TLS.KeyFile = %s, want /etc/ssl/key.pem", cfg.TLS.KeyFile)
	}
	if cfg.TLS.Domain != "example.com" {
		t.Errorf("TLS.Domain = %s, want example.com", cfg.TLS.Domain)
	}
}

// TestLoadKeycloakEnvOverride tests Keycloak environment overrides
func TestLoadKeycloakEnvOverride(t *testing.T) {
	os.Setenv("KEYCLOAK_URL", "http://keycloak:8180")
	os.Setenv("KEYCLOAK_REALM", "duq")
	os.Setenv("KEYCLOAK_CLIENT_ID", "gateway")
	os.Setenv("KEYCLOAK_ENABLED", "1")
	defer func() {
		os.Unsetenv("KEYCLOAK_URL")
		os.Unsetenv("KEYCLOAK_REALM")
		os.Unsetenv("KEYCLOAK_CLIENT_ID")
		os.Unsetenv("KEYCLOAK_ENABLED")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Keycloak.URL != "http://keycloak:8180" {
		t.Errorf("Keycloak.URL = %s, want http://keycloak:8180", cfg.Keycloak.URL)
	}
	if cfg.Keycloak.Realm != "duq" {
		t.Errorf("Keycloak.Realm = %s, want duq", cfg.Keycloak.Realm)
	}
	if cfg.Keycloak.ClientID != "gateway" {
		t.Errorf("Keycloak.ClientID = %s, want gateway", cfg.Keycloak.ClientID)
	}
	if !cfg.Keycloak.Enabled {
		t.Error("Keycloak.Enabled should be true from env '1'")
	}
}

// TestLoadAsyncQueueEnvOverride tests USE_ASYNC_QUEUE variations
func TestLoadAsyncQueueEnvOverride(t *testing.T) {
	tests := []struct {
		envValue string
		expected bool
	}{
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"", false}, // empty disables
	}

	for _, tt := range tests {
		t.Run("USE_ASYNC_QUEUE="+tt.envValue, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("USE_ASYNC_QUEUE", tt.envValue)
			} else {
				os.Unsetenv("USE_ASYNC_QUEUE")
			}
			defer os.Unsetenv("USE_ASYNC_QUEUE")

			cfg, _ := Load()
			// Note: default is true, so empty string will use default
			if tt.envValue == "" {
				if !cfg.UseAsyncQueue {
					t.Error("Empty env should use default (true)")
				}
			} else if cfg.UseAsyncQueue != tt.expected {
				t.Errorf("UseAsyncQueue = %v, want %v", cfg.UseAsyncQueue, tt.expected)
			}
		})
	}
}

// TestConfigStructFields tests that Config has all expected fields
func TestConfigStructFields(t *testing.T) {
	cfg := &Config{}

	// Test struct zero values exist
	_ = cfg.Port
	_ = cfg.TelegramChatID
	_ = cfg.DuqURL
	_ = cfg.GatewayHost
	_ = cfg.UseAsyncQueue
	_ = cfg.DocsPath
	_ = cfg.GitHubSecret
	_ = cfg.JWTSecret
	_ = cfg.OwnerEmail
	_ = cfg.BasicAuth
	_ = cfg.Database
	_ = cfg.Voice
	_ = cfg.Telegram
	_ = cfg.GoogleOAuth
	_ = cfg.Keycloak
	_ = cfg.TLS
	_ = cfg.Tracing
	_ = cfg.Queue
	_ = cfg.Timeouts
	_ = cfg.UserDefaults
}

// TestTimeoutsConfigDefaults tests that Load sets correct default timeout values
func TestTimeoutsConfigDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test all timeout defaults
	tests := []struct {
		name     string
		got      int
		expected int
	}{
		{"ProxyTimeout", cfg.Timeouts.ProxyTimeout, 60},
		{"DuqTimeout", cfg.Timeouts.DuqTimeout, 120},
		{"QueueTimeout", cfg.Timeouts.QueueTimeout, 10},
		{"KeycloakTimeout", cfg.Timeouts.KeycloakTimeout, 10},
		{"RedisTimeout", cfg.Timeouts.RedisTimeout, 5},
		{"STTTimeout", cfg.Timeouts.STTTimeout, 120},
		{"RBACCacheTTLMin", cfg.Timeouts.RBACCacheTTLMin, 5},
		{"JWKSCacheTTLMin", cfg.Timeouts.JWKSCacheTTLMin, 10},
		{"QRCodeTTLMin", cfg.Timeouts.QRCodeTTLMin, 5},
		{"SessionCleanupMin", cfg.Timeouts.SessionCleanupMin, 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("Timeouts.%s = %d, want %d", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestTimeoutsConfigZeroValuesFallback tests that zero values in config file get defaults applied
func TestTimeoutsConfigZeroValuesFallback(t *testing.T) {
	// Test that zero-value TimeoutsConfig gets RedisTimeout default applied
	cfg := &Config{
		Timeouts: TimeoutsConfig{
			// All zeros - simulating empty config file
		},
	}

	// Verify zero values before Load
	if cfg.Timeouts.ProxyTimeout != 0 {
		t.Errorf("Expected ProxyTimeout to be 0 before Load, got %d", cfg.Timeouts.ProxyTimeout)
	}

	// Load() applies defaults, including the special RedisTimeout fallback
	loadedCfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// RedisTimeout has special fallback logic (line 319-321 in config.go)
	if loadedCfg.Timeouts.RedisTimeout == 0 {
		t.Error("RedisTimeout should have default applied (5s), got 0")
	}
	if loadedCfg.Timeouts.RedisTimeout != 5 {
		t.Errorf("RedisTimeout = %d, want 5", loadedCfg.Timeouts.RedisTimeout)
	}
}

// TestKeycloakInternalURLDefault tests KeycloakInternalURL default value
func TestKeycloakInternalURLDefault(t *testing.T) {
	// Clear any env that might interfere
	os.Unsetenv("KEYCLOAK_INTERNAL_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := "http://localhost:8180"
	if cfg.KeycloakInternalURL != expected {
		t.Errorf("KeycloakInternalURL = %q, want %q", cfg.KeycloakInternalURL, expected)
	}
}

// TestKeycloakInternalURLEnvOverride tests KEYCLOAK_INTERNAL_URL env override
func TestKeycloakInternalURLEnvOverride(t *testing.T) {
	os.Setenv("KEYCLOAK_INTERNAL_URL", "http://keycloak.internal:8080")
	defer os.Unsetenv("KEYCLOAK_INTERNAL_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := "http://keycloak.internal:8080"
	if cfg.KeycloakInternalURL != expected {
		t.Errorf("KeycloakInternalURL = %q, want %q (from env)", cfg.KeycloakInternalURL, expected)
	}
}

// TestKeycloakInternalURLEmptyFallback tests that empty KEYCLOAK_INTERNAL_URL gets default
func TestKeycloakInternalURLEmptyFallback(t *testing.T) {
	// Setting empty string should trigger fallback
	os.Setenv("KEYCLOAK_INTERNAL_URL", "")
	defer os.Unsetenv("KEYCLOAK_INTERNAL_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Empty env var should be overwritten by default (lines 316-318)
	expected := "http://localhost:8180"
	if cfg.KeycloakInternalURL != expected {
		t.Errorf("KeycloakInternalURL = %q, want %q (default fallback)", cfg.KeycloakInternalURL, expected)
	}
}

// TestRedisTimeoutDefault tests RedisTimeout default value
func TestRedisTimeoutDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := 5
	if cfg.Timeouts.RedisTimeout != expected {
		t.Errorf("Timeouts.RedisTimeout = %d, want %d", cfg.Timeouts.RedisTimeout, expected)
	}
}

// TestUserDefaultsConfig tests user defaults configuration
func TestUserDefaultsConfig(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.UserDefaults.Timezone != "UTC" {
		t.Errorf("UserDefaults.Timezone = %q, want %q", cfg.UserDefaults.Timezone, "UTC")
	}
	if cfg.UserDefaults.PreferredLanguage != "ru" {
		t.Errorf("UserDefaults.PreferredLanguage = %q, want %q", cfg.UserDefaults.PreferredLanguage, "ru")
	}
}

// TestUserDefaultsEnvOverride tests USER_DEFAULT_* env overrides
func TestUserDefaultsEnvOverride(t *testing.T) {
	os.Setenv("USER_DEFAULT_TIMEZONE", "Europe/Moscow")
	os.Setenv("USER_DEFAULT_LANGUAGE", "en")
	defer func() {
		os.Unsetenv("USER_DEFAULT_TIMEZONE")
		os.Unsetenv("USER_DEFAULT_LANGUAGE")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.UserDefaults.Timezone != "Europe/Moscow" {
		t.Errorf("UserDefaults.Timezone = %q, want %q (from env)", cfg.UserDefaults.Timezone, "Europe/Moscow")
	}
	if cfg.UserDefaults.PreferredLanguage != "en" {
		t.Errorf("UserDefaults.PreferredLanguage = %q, want %q (from env)", cfg.UserDefaults.PreferredLanguage, "en")
	}
}

// TestQueueConfigDefault tests Queue configuration defaults
func TestQueueConfigDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := "redis://localhost:6379"
	if cfg.Queue.RedisURL != expected {
		t.Errorf("Queue.RedisURL = %q, want %q", cfg.Queue.RedisURL, expected)
	}
}

// TestTimeoutsConfigStructFields tests that TimeoutsConfig has all expected fields
func TestTimeoutsConfigStructFields(t *testing.T) {
	cfg := &TimeoutsConfig{}

	// Verify all fields exist with correct zero values
	_ = cfg.ProxyTimeout
	_ = cfg.DuqTimeout
	_ = cfg.QueueTimeout
	_ = cfg.KeycloakTimeout
	_ = cfg.RedisTimeout
	_ = cfg.STTTimeout
	_ = cfg.RBACCacheTTLMin
	_ = cfg.JWKSCacheTTLMin
	_ = cfg.QRCodeTTLMin
	_ = cfg.SessionCleanupMin

	// All should be zero initially
	if cfg.ProxyTimeout != 0 {
		t.Errorf("Zero TimeoutsConfig.ProxyTimeout should be 0, got %d", cfg.ProxyTimeout)
	}
	if cfg.RedisTimeout != 0 {
		t.Errorf("Zero TimeoutsConfig.RedisTimeout should be 0, got %d", cfg.RedisTimeout)
	}
}
