package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// Default values for user preferences (used when user not found in DB)
const (
	DefaultTimezone          = "UTC"
	DefaultPreferredLanguage = "ru"
)

type BasicAuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type VoiceConfig struct {
	STTCommand     string `json:"stt_command"`      // Path to whisper-stt
	TTSVoice       string `json:"tts_voice"`        // e.g., ru-RU-DmitryNeural
	SessionTTLDays int    `json:"session_ttl_days"` // Mobile session TTL in days
}

type TelegramConfig struct {
	BotToken string `json:"bot_token"` // Telegram bot token for downloading files
}

type GoogleOAuthConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
}

type KeycloakConfig struct {
	URL          string `json:"url"`           // e.g., http://localhost:8180
	Realm        string `json:"realm"`         // e.g., duq
	ClientID     string `json:"client_id"`     // e.g., duq-gateway
	ClientSecret string `json:"client_secret"` // from Keycloak
	Enabled      bool   `json:"enabled"`       // Enable Keycloak auth
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	// CertMagic (Let's Encrypt) settings
	Domain  string `json:"domain"`   // Domain for auto-cert (e.g., on-za-menya.online)
	Email   string `json:"email"`    // Email for Let's Encrypt notifications
	DataDir string `json:"data_dir"` // Directory to store certificates (default: /var/lib/duq-gateway/certs)
}

type TracingConfig struct {
	Enabled  bool   `json:"enabled"`
	RedisURL string `json:"redis_url"`
	Channel  string `json:"channel"`
}

type QueueConfig struct {
	RedisURL string `json:"redis_url"`
}

// TimeoutsConfig holds all timeout configurations (in seconds unless noted)
type TimeoutsConfig struct {
	// HTTP client timeouts
	ProxyTimeout   int `json:"proxy_timeout"`   // Proxy handler timeout (default: 60s)
	DuqTimeout  int `json:"duq_timeout"`  // Duq API client timeout (default: 120s)
	QueueTimeout   int `json:"queue_timeout"`   // Redis queue timeout (default: 10s)
	KeycloakTimeout int `json:"keycloak_timeout"` // Keycloak HTTP client timeout (default: 10s)
	RedisTimeout   int `json:"redis_timeout"`   // Redis operation timeout in seconds (default: 5)

	// STT/TTS timeouts
	STTTimeout int `json:"stt_timeout"` // Speech-to-text timeout (default: 120s)

	// Cache TTLs (in minutes)
	RBACCacheTTLMin    int `json:"rbac_cache_ttl_min"`    // RBAC cache TTL in minutes (default: 5)
	JWKSCacheTTLMin    int `json:"jwks_cache_ttl_min"`    // Keycloak JWKS cache TTL in minutes (default: 10)
	QRCodeTTLMin       int `json:"qr_code_ttl_min"`       // QR code validity in minutes (default: 5)
	SessionCleanupMin  int `json:"session_cleanup_min"`   // Session cleanup interval in minutes (default: 60)
}

// UserDefaultsConfig holds default values for new users
type UserDefaultsConfig struct {
	Timezone          string `json:"timezone"`           // Default timezone (e.g., "UTC", "Europe/Moscow")
	PreferredLanguage string `json:"preferred_language"` // Default language (e.g., "ru", "en")
}

type Config struct {
	Port               string            `json:"port"`
	TelegramChatID     string            `json:"telegram_chat_id"`
	DuqURL          string            `json:"duq_url"`
	GatewayHost        string            `json:"gateway_host"`         // Phase 3: Self address for callbacks
	UseAsyncQueue      bool              `json:"use_async_queue"`      // Phase 3: Use async queue instead of sync chat
	KeycloakInternalURL string           `json:"keycloak_internal_url"` // Internal Keycloak URL for proxy (default: http://localhost:8180)
	DocsPath           string            `json:"docs_path"`
	GitHubSecret       string            `json:"github_secret"`
	JWTSecret          string            `json:"jwt_secret"` // JWT secret for authentication
	OwnerEmail         string            `json:"owner_email"`
	BasicAuth          BasicAuthConfig   `json:"basic_auth"`
	Database           DatabaseConfig    `json:"database"`
	Voice              VoiceConfig       `json:"voice"`
	Telegram           TelegramConfig    `json:"telegram"`
	GoogleOAuth        GoogleOAuthConfig `json:"google_oauth"`
	Keycloak           KeycloakConfig    `json:"keycloak"`
	TLS                TLSConfig         `json:"tls"`
	Tracing            TracingConfig     `json:"tracing"`
	Queue              QueueConfig       `json:"queue"`
	Timeouts           TimeoutsConfig    `json:"timeouts"`
	UserDefaults       UserDefaultsConfig `json:"user_defaults"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:           "8082",
		TelegramChatID: "764733417",
		DuqURL:      "http://localhost:8081",
		GatewayHost:    "localhost:8082", // Phase 3: Default to localhost
		UseAsyncQueue:  true,             // Phase 3: Default to async (new behavior)
		Database: DatabaseConfig{
			Host: "localhost",
			Port: 5433,
			User: "duq",
			Name: "duq",
		},
		Voice: VoiceConfig{
			STTCommand:     "/usr/local/bin/whisper-stt",
			TTSVoice:       "ru-RU-DmitryNeural",
			SessionTTLDays: 30,
		},
		Tracing: TracingConfig{
			Enabled:  true,
			RedisURL: "redis://localhost:6379",
			Channel:  "duq:traces",
		},
		Queue: QueueConfig{
			RedisURL: "redis://localhost:6379",
		},
		Timeouts: TimeoutsConfig{
			ProxyTimeout:      60,   // 60 seconds
			DuqTimeout:     120,  // 120 seconds
			QueueTimeout:      10,   // 10 seconds
			KeycloakTimeout:   10,   // 10 seconds
			RedisTimeout:      5,    // 5 seconds
			STTTimeout:        120,  // 120 seconds
			RBACCacheTTLMin:   5,    // 5 minutes
			JWKSCacheTTLMin:   10,   // 10 minutes
			QRCodeTTLMin:      5,    // 5 minutes
			SessionCleanupMin: 60,   // 60 minutes (1 hour)
		},
		UserDefaults: UserDefaultsConfig{
			Timezone:          "UTC",
			PreferredLanguage: "ru",
		},
	}

	// Try to load from config file
	configPaths := []string{
		"/etc/duq-gateway/config.json",
		filepath.Join(os.Getenv("HOME"), ".config/duq-gateway/config.json"),
		"config.json",
	}

	for _, path := range configPaths {
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
			break
		}
	}

	// Override from environment
	if port := os.Getenv("JARVIS_PORT"); port != "" {
		cfg.Port = port
	}
	if chatID := os.Getenv("JARVIS_TELEGRAM_CHAT_ID"); chatID != "" {
		cfg.TelegramChatID = chatID
	}
	if url := os.Getenv("JARVIS_URL"); url != "" {
		cfg.DuqURL = url
	}

	// Phase 3: Gateway host for callbacks
	if host := os.Getenv("GATEWAY_HOST"); host != "" {
		cfg.GatewayHost = host
	}
	if async := os.Getenv("USE_ASYNC_QUEUE"); async != "" {
		cfg.UseAsyncQueue = async == "true" || async == "1"
	}

	// Database env overrides
	if host := os.Getenv("JARVIS_DB_HOST"); host != "" {
		cfg.Database.Host = host
	}
	if port := os.Getenv("JARVIS_DB_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Database.Port = p
		}
	}
	if user := os.Getenv("JARVIS_DB_USER"); user != "" {
		cfg.Database.User = user
	}
	if pass := os.Getenv("JARVIS_DB_PASSWORD"); pass != "" {
		cfg.Database.Password = pass
	}
	if name := os.Getenv("JARVIS_DB_NAME"); name != "" {
		cfg.Database.Name = name
	}

	// Voice env overrides
	if stt := os.Getenv("JARVIS_STT_COMMAND"); stt != "" {
		cfg.Voice.STTCommand = stt
	}
	if tts := os.Getenv("JARVIS_TTS_VOICE"); tts != "" {
		cfg.Voice.TTSVoice = tts
	}

	// Telegram env overrides
	if botToken := os.Getenv("TELEGRAM_BOT_TOKEN"); botToken != "" {
		cfg.Telegram.BotToken = botToken
	}

	// Owner email override
	if ownerEmail := os.Getenv("OWNER_EMAIL"); ownerEmail != "" {
		cfg.OwnerEmail = ownerEmail
	}

	// Google OAuth env overrides
	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		cfg.GoogleOAuth.ClientID = clientID
	}
	if clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET"); clientSecret != "" {
		cfg.GoogleOAuth.ClientSecret = clientSecret
	}
	if redirectURI := os.Getenv("GOOGLE_REDIRECT_URI"); redirectURI != "" {
		cfg.GoogleOAuth.RedirectURI = redirectURI
	}

	// JWT secret env override
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		cfg.JWTSecret = jwtSecret
	}

	// TLS env overrides
	if tlsEnabled := os.Getenv("TLS_ENABLED"); tlsEnabled == "true" || tlsEnabled == "1" {
		cfg.TLS.Enabled = true
	}
	if certFile := os.Getenv("TLS_CERT_FILE"); certFile != "" {
		cfg.TLS.CertFile = certFile
	}
	if keyFile := os.Getenv("TLS_KEY_FILE"); keyFile != "" {
		cfg.TLS.KeyFile = keyFile
	}
	if domain := os.Getenv("TLS_DOMAIN"); domain != "" {
		cfg.TLS.Domain = domain
	}
	if email := os.Getenv("TLS_EMAIL"); email != "" {
		cfg.TLS.Email = email
	}
	if dataDir := os.Getenv("TLS_DATA_DIR"); dataDir != "" {
		cfg.TLS.DataDir = dataDir
	}

	// Tracing env overrides
	if tracingEnabled := os.Getenv("TRACING_ENABLED"); tracingEnabled != "" {
		cfg.Tracing.Enabled = tracingEnabled == "true" || tracingEnabled == "1"
	}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		cfg.Tracing.RedisURL = redisURL
	}
	if channel := os.Getenv("TRACE_CHANNEL"); channel != "" {
		cfg.Tracing.Channel = channel
	}

	// Keycloak env overrides
	if keycloakURL := os.Getenv("KEYCLOAK_URL"); keycloakURL != "" {
		cfg.Keycloak.URL = keycloakURL
	}
	if keycloakRealm := os.Getenv("KEYCLOAK_REALM"); keycloakRealm != "" {
		cfg.Keycloak.Realm = keycloakRealm
	}
	if keycloakClientID := os.Getenv("KEYCLOAK_CLIENT_ID"); keycloakClientID != "" {
		cfg.Keycloak.ClientID = keycloakClientID
	}
	if keycloakClientSecret := os.Getenv("KEYCLOAK_CLIENT_SECRET"); keycloakClientSecret != "" {
		cfg.Keycloak.ClientSecret = keycloakClientSecret
	}
	if keycloakEnabled := os.Getenv("KEYCLOAK_ENABLED"); keycloakEnabled != "" {
		cfg.Keycloak.Enabled = keycloakEnabled == "true" || keycloakEnabled == "1"
	}

	// User defaults env overrides
	if timezone := os.Getenv("USER_DEFAULT_TIMEZONE"); timezone != "" {
		cfg.UserDefaults.Timezone = timezone
	}
	if lang := os.Getenv("USER_DEFAULT_LANGUAGE"); lang != "" {
		cfg.UserDefaults.PreferredLanguage = lang
	}

	// Keycloak internal URL env override
	if keycloakInternalURL := os.Getenv("KEYCLOAK_INTERNAL_URL"); keycloakInternalURL != "" {
		cfg.KeycloakInternalURL = keycloakInternalURL
	}

	// Apply defaults for new fields (in case config file had zeros/empty values)
	if cfg.KeycloakInternalURL == "" {
		cfg.KeycloakInternalURL = "http://localhost:8180"
	}
	if cfg.Timeouts.RedisTimeout == 0 {
		cfg.Timeouts.RedisTimeout = 5
	}

	return cfg, nil
}
