package main

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caddyserver/certmagic"

	"duq-gateway/internal/channels"
	"duq-gateway/internal/config"
	"duq-gateway/internal/credentials"
	"duq-gateway/internal/db"
	"duq-gateway/internal/handlers"
	"duq-gateway/internal/middleware"
	"duq-gateway/internal/queue"
	"duq-gateway/internal/rbac"
	"duq-gateway/internal/registration"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database connection
	dbClient, err := db.New(db.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		Name:     cfg.Database.Name,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()

	// Background goroutine for session cleanup (configurable interval)
	sessionCleanupInterval := time.Duration(cfg.Timeouts.SessionCleanupMin) * time.Minute
	if sessionCleanupInterval == 0 {
		sessionCleanupInterval = 60 * time.Minute // fallback default (1 hour)
	}
	go func() {
		ticker := time.NewTicker(sessionCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			count, err := dbClient.DeleteExpiredSessions()
			if err != nil {
				log.Printf("[cleanup] Error deleting expired sessions: %v", err)
			} else if count > 0 {
				log.Printf("[cleanup] Deleted %d expired sessions", count)
			}
		}
	}()

	// Initialize Redis queue client (Gateway → Redis direct push)
	queueClient, err := queue.NewClient(cfg.Queue.RedisURL, cfg.Timeouts.RedisTimeout)
	if err != nil {
		log.Fatalf("Failed to connect to Redis queue: %v", err)
	}
	defer queueClient.Close()

	// Initialize Keycloak client
	handlers.InitKeycloakClient(cfg)

	// Initialize services
	rbacService := rbac.NewService(dbClient.DB(), cfg.Timeouts.RBACCacheTTLMin)
	credService := credentials.NewService(dbClient.DB())

	// Build channel router (SOLID: easily extensible with new channels)
	// Note: TTS is done by Duq, channel only converts MP3→OGG
	channelRouter := channels.NewBuilder().
		WithTelegram(cfg.Telegram.BotToken).
		WithEmail().
		WithObsidian().
		WithSilent().
		Build()

	log.Printf("[channels] Router initialized with telegram, email, obsidian, silent channels")

	// Create unified registration service
	registrationService := registration.NewService(cfg, dbClient)
	log.Printf("[registration] Unified registration service initialized")

	// Create Telegram handler with full dependencies
	telegramDeps := &handlers.TelegramDeps{
		Config:              cfg,
		QueueClient:         queueClient,
		RBACService:         rbacService,
		CredService:         credService,
		ChannelRouter:       channelRouter,
		DBClient:            dbClient,
		RegistrationService: registrationService,
	}

	// Google OAuth dependencies
	oauthDeps := &handlers.GoogleOAuthDeps{
		Config:      cfg,
		CredService: credService,
		SendMessage: func(chatID int64, text string) error {
			return handlers.SendTelegramMessage(cfg, chatID, text)
		},
	}

	// Phase 3: Callback dependencies (for async task results)
	callbackDeps := handlers.NewCallbackDeps(cfg, channelRouter)

	// Rate limiters for public endpoints (prevent DoS)
	// Telegram: 60 req/min per IP (Telegram servers use few IPs)
	telegramLimiter := middleware.NewRateLimiter(60, time.Minute)
	defer telegramLimiter.Stop()
	// Webhooks: 30 req/min per IP
	webhookLimiter := middleware.NewRateLimiter(30, time.Minute)
	defer webhookLimiter.Stop()
	// Auth: 10 req/min per IP (prevent brute force)
	authLimiter := middleware.NewRateLimiter(10, time.Minute)
	defer authLimiter.Stop()

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", handlers.Health)

	// Documentation (protected with BasicAuth)
	mux.HandleFunc("GET /docs", middleware.BasicAuth(cfg, handlers.Docs(cfg)))
	mux.HandleFunc("GET /docs/", middleware.BasicAuth(cfg, handlers.Docs(cfg)))
	mux.HandleFunc("GET /api/docs/list", handlers.DocsList(cfg))              // Public endpoint for docs sidebar
	mux.HandleFunc("GET /api/docs/{name}/content", handlers.DocsContent(cfg)) // Public endpoint for raw markdown

	// Webhook endpoints (rate limited to prevent DoS)
	// All webhooks push to Redis queue - no direct HTTP to backend
	calendarDeps := &handlers.CalendarDeps{Config: cfg, QueueClient: queueClient}
	gmailDeps := &handlers.GmailDeps{Config: cfg, QueueClient: queueClient}
	githubDeps := &handlers.GitHubDeps{Config: cfg, QueueClient: queueClient}
	customDeps := &handlers.CustomDeps{Config: cfg, QueueClient: queueClient}
	mcpDeps := &handlers.MCPDeps{Config: cfg, QueueClient: queueClient}

	mux.HandleFunc("POST /api/calendar", middleware.RateLimitFunc(webhookLimiter, handlers.Calendar(calendarDeps)))
	mux.HandleFunc("POST /api/gmail", middleware.RateLimitFunc(webhookLimiter, handlers.Gmail(gmailDeps)))
	mux.HandleFunc("POST /api/github", middleware.RateLimitFunc(webhookLimiter, handlers.GitHub(githubDeps)))
	mux.HandleFunc("POST /api/custom", middleware.RateLimitFunc(webhookLimiter, handlers.Custom(customDeps)))

	// MCP endpoint - synchronous, waits for response (used by Claude Code)
	// Protected by Keycloak service account auth (client_credentials flow)
	// Empty list = any valid token from our Keycloak realm is accepted
	mcpAllowedClients := []string{} // TODO: add "duq-mcp" when service account created
	mux.HandleFunc("POST /api/mcp", middleware.KeycloakServiceAuth(cfg, mcpAllowedClients, handlers.MCP(mcpDeps)))

	// Telegram endpoints (rate limited)
	mux.HandleFunc("POST /api/telegram/webhook", middleware.RateLimitFunc(telegramLimiter, handlers.TelegramWithDeps(telegramDeps)))
	mux.HandleFunc("POST /api/telegram/send", handlers.TelegramSend(cfg))

	// Voice endpoint REMOVED - all requests through Redis queue

	// Google OAuth endpoints
	mux.HandleFunc("GET /api/auth/google/callback", handlers.GoogleOAuthCallback(oauthDeps))
	mux.HandleFunc("GET /api/auth/google/link", handlers.GetOAuthLinkHandler(cfg))
	mux.HandleFunc("POST /api/oauth/google/initiate", handlers.InitiateOAuthHandler(oauthDeps))

	// Keycloak OIDC endpoints (единый SSO)
	mux.HandleFunc("GET /api/auth/keycloak/login", handlers.KeycloakLogin(cfg))
	mux.HandleFunc("GET /api/auth/keycloak/callback", handlers.KeycloakCallback(cfg, dbClient))
	mux.HandleFunc("POST /api/auth/keycloak/refresh", handlers.KeycloakRefresh(cfg))
	mux.HandleFunc("POST /api/auth/keycloak/logout", handlers.KeycloakLogout(cfg))
	mux.HandleFunc("GET /api/auth/keycloak/userinfo", handlers.KeycloakUserInfo(cfg))

	// Public Registration endpoints (rate limited to prevent brute force)
	registrationDeps := handlers.NewRegistrationDeps(cfg, dbClient)
	mux.HandleFunc("POST /api/auth/register", middleware.RateLimitFunc(authLimiter, handlers.Register(registrationDeps)))
	mux.HandleFunc("GET /api/auth/verify-email", middleware.RateLimitFunc(authLimiter, handlers.VerifyEmail(registrationDeps)))
	mux.HandleFunc("POST /api/auth/resend-verification", middleware.RateLimitFunc(authLimiter, handlers.ResendVerification(registrationDeps)))

	// QR Authentication endpoints (rate limited)
	mux.HandleFunc("POST /api/auth/qr/generate", middleware.KeycloakAuth(cfg, dbClient, handlers.QRGenerate(cfg, dbClient)))
	mux.HandleFunc("POST /api/auth/qr/verify", middleware.RateLimitFunc(authLimiter, handlers.QRVerify(cfg, dbClient)))

	// User Management endpoints (Keycloak SSO)
	mux.HandleFunc("GET /api/users", middleware.KeycloakAuth(cfg, dbClient, handlers.ListUsers(dbClient)))
	mux.HandleFunc("POST /api/users", middleware.KeycloakAuth(cfg, dbClient, handlers.CreateUser(dbClient)))
	mux.HandleFunc("PUT /api/users/{id}", middleware.KeycloakAuth(cfg, dbClient, handlers.UpdateUser(dbClient)))
	mux.HandleFunc("DELETE /api/users/{id}", middleware.KeycloakAuth(cfg, dbClient, handlers.DeleteUser(dbClient)))

	// ALL PROXY ENDPOINTS REMOVED
	// All requests to Duq backend go through Redis queue only
	// Gateway pushes to duq:requests, worker (backend) processes and responds

	// Phase 3: Duq callback endpoint (receives async task results from worker)
	mux.HandleFunc("POST /api/duq/callback", handlers.DuqCallback(callbackDeps))

	// Admin Panel Reverse Proxy (configurable via ADMIN_URL env)
	adminURL, _ := url.Parse(cfg.AdminURL)
	adminProxy := httputil.NewSingleHostReverseProxy(adminURL)
	adminProxy.FlushInterval = -1 // Enable streaming for SSE

	// Rewrite Location headers: /login -> /admin/login
	adminProxy.ModifyResponse = func(resp *http.Response) error {
		if location := resp.Header.Get("Location"); location != "" {
			// If backend returns relative redirect, prepend /admin
			if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "/admin") {
				resp.Header.Set("Location", "/admin"+location)
			}
		}
		return nil
	}

	// Custom SSE proxy handler - manually copies response with explicit flushing
	sseProxyHandler := func(w http.ResponseWriter, r *http.Request) {
		// Build target URL - strip /admin prefix
		targetPath := strings.TrimPrefix(r.URL.Path, "/admin")
		targetURL := adminURL.Scheme + "://" + adminURL.Host + targetPath

		// Create proxied request
		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
		if err != nil {
			http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
			return
		}

		// Copy headers
		for key, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		// Make request to backend (with reasonable timeout for SSE to prevent DoS)
		// 5 minutes is generous for SSE streaming but prevents infinite hangs
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, "Failed to proxy request", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)

		// Use ResponseController for flushing (Go 1.20+)
		rc := http.NewResponseController(w)

		// Stream data with immediate flushing
		buf := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					break
				}
				rc.Flush()
			}
			if err != nil {
				break
			}
		}
	}

	adminHandler := func(w http.ResponseWriter, r *http.Request) {
		// Strip /admin prefix, keep the rest
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/admin")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.Host = adminURL.Host
		adminProxy.ServeHTTP(w, r)
	}

	// Register admin routes - duq-admin handles auth via Keycloak cookies
	// SSE endpoints use custom proxy for streaming
	mux.HandleFunc("GET /admin/traces/stream", sseProxyHandler)
	mux.HandleFunc("GET /admin/api/system/logs/stream", sseProxyHandler)
	mux.HandleFunc("GET /admin/", adminHandler)
	mux.HandleFunc("POST /admin/", adminHandler)

	// Duq Backend Reverse Proxy for monitoring API (direct access, not through Redis queue)
	duqBackendURL, _ := url.Parse(cfg.DuqURL)
	duqBackendProxy := httputil.NewSingleHostReverseProxy(duqBackendURL)
	monitoringHandler := func(w http.ResponseWriter, r *http.Request) {
		r.Host = duqBackendURL.Host
		duqBackendProxy.ServeHTTP(w, r)
	}
	mux.HandleFunc("GET /api/monitoring/", monitoringHandler)

	// Keycloak Reverse Proxy (for mobile app HTTPS OAuth)
	// Uses cfg.KeycloakInternalURL for internal proxy target
	keycloakProxyURL := cfg.KeycloakInternalURL
	keycloakURL, _ := url.Parse(keycloakProxyURL)
	keycloakProxy := httputil.NewSingleHostReverseProxy(keycloakURL)
	keycloakProxy.Director = func(req *http.Request) {
		req.URL.Scheme = keycloakURL.Scheme
		req.URL.Host = keycloakURL.Host
		req.Host = keycloakURL.Host
		// Tell Keycloak the original request was HTTPS
		req.Header.Set("X-Forwarded-Proto", "https")
		// Use domain from config (TLS.Domain or fallback to Host header)
		forwardedHost := cfg.TLS.Domain
		if forwardedHost == "" {
			forwardedHost = req.Host // fallback to original Host header
		}
		req.Header.Set("X-Forwarded-Host", forwardedHost)
	}
	keycloakHandler := func(w http.ResponseWriter, r *http.Request) {
		keycloakProxy.ServeHTTP(w, r)
	}
	mux.HandleFunc("GET /realms/", keycloakHandler)
	mux.HandleFunc("POST /realms/", keycloakHandler)
	log.Printf("[keycloak] Reverse proxy enabled: /realms/* -> %s", keycloakProxyURL)

	// Root redirect to admin panel
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Security headers middleware (HSTS, X-Frame-Options, CSP, etc.)
	securityConfig := middleware.DefaultSecurityHeadersConfig()
	// Adjust for development if TLS is disabled
	if !cfg.TLS.Enabled {
		securityConfig.HSTSEnabled = false // HSTS only makes sense with HTTPS
	}

	// CSRF protection (disabled for webhooks and Bearer token APIs)
	csrfConfig := middleware.DefaultCSRFConfig()
	csrfConfig.CookieSecure = cfg.TLS.Enabled // Secure cookies only with HTTPS
	// Exclude admin login from CSRF (unauthenticated, Keycloak handles security)
	csrfConfig.ExcludePaths = append(csrfConfig.ExcludePaths, "/admin/login", "/admin/logout")
	csrfStore := middleware.NewCSRFStore(24 * time.Hour)

	// Apply middlewares in order: Security Headers → CSRF → Logger
	handler := middleware.SecurityHeaders(securityConfig)(mux)
	handler = middleware.CSRF(csrfConfig, csrfStore)(handler)
	handler = middleware.Logger(handler)

	// Graceful shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start internal HTTP server on :8082 for container-to-container communication (Duq→Gateway)
	// In Docker, this is accessible via the duq-network; external access controlled by port mapping
	go func() {
		internalServer := &http.Server{
			Addr:    "0.0.0.0:" + cfg.Port,
			Handler: handler,
		}
		log.Printf("Internal HTTP server starting on 0.0.0.0:%s", cfg.Port)
		if err := internalServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Internal HTTP server error: %v", err)
		}
	}()

	// Start server based on TLS configuration
	if cfg.TLS.Enabled && cfg.TLS.Domain != "" {
		// CertMagic: automatic Let's Encrypt certificates
		log.Printf("CertMagic enabled for domain: %s", cfg.TLS.Domain)

		// Configure certmagic
		certmagic.DefaultACME.Agreed = true
		if cfg.TLS.Email != "" {
			certmagic.DefaultACME.Email = cfg.TLS.Email
		}
		if cfg.TLS.DataDir != "" {
			certmagic.Default.Storage = &certmagic.FileStorage{Path: cfg.TLS.DataDir}
		} else {
			certmagic.Default.Storage = &certmagic.FileStorage{Path: "/var/lib/duq-gateway/certs"}
		}

		// Start HTTPS server with certmagic
		// certmagic.HTTPS() handles both :443 and :80 (for ACME challenges + HTTP→HTTPS redirect)
		log.Printf("Duq Gateway starting HTTPS on :443 (domain: %s)", cfg.TLS.Domain)
		if err := certmagic.HTTPS([]string{cfg.TLS.Domain}, handler); err != nil {
			log.Fatalf("CertMagic HTTPS error: %v", err)
		}

	} else if cfg.TLS.Enabled && cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		// Manual TLS with provided certificates
		log.Printf("Manual TLS enabled, using cert: %s, key: %s", cfg.TLS.CertFile, cfg.TLS.KeyFile)

		server := &http.Server{
			Addr:    ":443",
			Handler: handler,
		}

		// Start HTTP→HTTPS redirect server on port 80
		go func() {
			redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := "https://" + r.Host + r.URL.Path
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			})
			log.Println("HTTP redirect server starting on :80")
			if err := http.ListenAndServe(":80", redirectHandler); err != nil {
				log.Printf("HTTP redirect server error: %v", err)
			}
		}()

		log.Println("Duq Gateway starting HTTPS on :443")
		if err := server.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}

	} else {
		// Plain HTTP
		server := &http.Server{
			Addr:    ":" + cfg.Port,
			Handler: handler,
		}

		log.Printf("Duq Gateway starting HTTP on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}

	<-ctx.Done()
}
