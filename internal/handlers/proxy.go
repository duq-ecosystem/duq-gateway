package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"duq-gateway/internal/config"
	"duq-gateway/internal/db"
)

// duqProxyClient is a shared HTTP client with timeout for proxy requests
// Initialized via InitProxyClient with config
var duqProxyClient *http.Client

// InitProxyClient initializes the proxy HTTP client with configured timeout
func InitProxyClient(cfg *config.Config) {
	timeout := time.Duration(cfg.Timeouts.ProxyTimeout) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second // fallback default
	}
	duqProxyClient = &http.Client{
		Timeout: timeout,
	}
}

// ProxyDeps contains dependencies for proxy handlers
type ProxyDeps struct {
	Config   *config.Config
	DBClient *db.Client
}

// isAdmin checks if user has admin or root role
func isAdmin(role string) bool {
	return role == "root" || role == "admin"
}

// enforceUserIDAccess ensures non-admin users can only access their own data
func enforceUserIDAccess(requestedUserID string, contextUserID int64, role string) bool {
	// Admins can access any user_id
	if isAdmin(role) {
		return true
	}

	// Non-admins can only access their own user_id
	requested, err := strconv.ParseInt(requestedUserID, 10, 64)
	if err != nil {
		return false
	}
	return requested == contextUserID
}

// proxyToDuq forwards request to duq backend with RBAC enforcement
func proxyToDuq(w http.ResponseWriter, r *http.Request, duqURL string, enforceUserID bool) {
	// Get user context from JWT middleware
	userID, ok := r.Context().Value("user_id").(int64)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	role, ok := r.Context().Value("role").(string)
	if !ok {
		role = "public"
	}

	// Parse query params
	query := r.URL.Query()

	// RBAC: Enforce user_id filtering for non-admin users
	if enforceUserID {
		requestedUserID := query.Get("user_id")

		// If no user_id provided, set it to current user
		if requestedUserID == "" {
			query.Set("user_id", strconv.FormatInt(userID, 10))
		} else {
			// Verify access
			if !enforceUserIDAccess(requestedUserID, userID, role) {
				http.Error(w, "Forbidden: cannot access other users' data", http.StatusForbidden)
				return
			}
		}
	}

	// Build full URL
	fullURL := duqURL + "?" + query.Encode()
	log.Printf("[proxy] %s %s (user=%d, role=%s)", r.Method, fullURL, userID, role)

	// Create new request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, fullURL, r.Body)
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

	// Execute request
	resp, err := duqProxyClient.Do(proxyReq)
	if err != nil {
		log.Printf("[proxy] Error: %v", err)
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

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy body
	io.Copy(w, resp.Body)
}

// ProxyWorkflowsList proxies GET /api/workflows with RBAC
func ProxyWorkflowsList(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/workflows"
		proxyToDuq(w, r, duqURL, true) // enforce user_id
	}
}

// ProxyWorkflowCreate proxies POST /api/workflows with RBAC
func ProxyWorkflowCreate(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user context
		userID, ok := r.Context().Value("user_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		// Parse JSON to inject user_id
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Force user_id to current user
		query := r.URL.Query()
		query.Set("user_id", strconv.FormatInt(userID, 10))

		duqURL := deps.Config.DuqURL + "/api/workflows?" + query.Encode()

		// Create new request with modified body
		proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", duqURL, strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
			return
		}

		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			log.Printf("[proxy] Workflow create error: %v", err)
			http.Error(w, "Failed to create workflow", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyWorkflowGet proxies GET /api/workflows/{id}
func ProxyWorkflowGet(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workflowID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/workflows/" + workflowID
		proxyToDuq(w, r, duqURL, false) // no user_id enforcement on GET by ID
	}
}

// ProxyWorkflowDelete proxies DELETE /api/workflows/{id}
func ProxyWorkflowDelete(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workflowID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/workflows/" + workflowID
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyWorkflowRun proxies POST /api/workflows/{id}/run
func ProxyWorkflowRun(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workflowID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/workflows/" + workflowID + "/run"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyRecurringList proxies GET /api/recurring with RBAC
func ProxyRecurringList(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/recurring"
		proxyToDuq(w, r, duqURL, true) // enforce user_id
	}
}

// ProxyRecurringCreate proxies POST /api/recurring with RBAC
func ProxyRecurringCreate(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		query := r.URL.Query()
		query.Set("user_id", strconv.FormatInt(userID, 10))

		duqURL := deps.Config.DuqURL + "/api/recurring?" + query.Encode()

		body, _ := io.ReadAll(r.Body)
		proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", duqURL, strings.NewReader(string(body)))
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			http.Error(w, "Failed to create recurring task", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyRecurringDelete proxies DELETE /api/recurring/{id}
func ProxyRecurringDelete(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/recurring/" + taskID
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyCortexSearch proxies GET /api/cortex/search with RBAC
func ProxyCortexSearch(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cortex searches are implicitly scoped by user via backend
		duqURL := deps.Config.DuqURL + "/api/cortex/search"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyCortexStore proxies POST /api/cortex/store with RBAC
func ProxyCortexStore(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Read and modify body to inject user_id
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Inject user_id
		payload["user_id"] = strconv.FormatInt(userID, 10)

		modifiedBody, _ := json.Marshal(payload)
		duqURL := deps.Config.DuqURL + "/api/cortex/store"

		proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", duqURL, strings.NewReader(string(modifiedBody)))
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			http.Error(w, "Failed to store memory", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyQueueStats proxies GET /api/queue/stats/overview
func ProxyQueueStats(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Queue stats are global, available to all authenticated users
		duqURL := deps.Config.DuqURL + "/api/queue/stats/overview"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyHeartbeatConfig proxies GET /api/heartbeat/config with RBAC
func ProxyHeartbeatConfig(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/heartbeat/config"
		proxyToDuq(w, r, duqURL, true) // enforce user_id
	}
}

// ProxyHeartbeatUpdate proxies PUT /api/heartbeat/config with RBAC
func ProxyHeartbeatUpdate(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		query := r.URL.Query()
		query.Set("user_id", strconv.FormatInt(userID, 10))

		duqURL := deps.Config.DuqURL + "/api/heartbeat/config?" + query.Encode()

		body, _ := io.ReadAll(r.Body)
		proxyReq, _ := http.NewRequestWithContext(r.Context(), "PUT", duqURL, strings.NewReader(string(body)))
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			http.Error(w, "Failed to update heartbeat config", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyHeartbeatRun proxies POST /api/heartbeat/run with RBAC
func ProxyHeartbeatRun(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		query := r.URL.Query()
		query.Set("user_id", strconv.FormatInt(userID, 10))

		duqURL := deps.Config.DuqURL + "/api/heartbeat/run?" + query.Encode()

		proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", duqURL, nil)

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			http.Error(w, "Failed to run heartbeat", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyHeartbeatChecks proxies GET /api/heartbeat/checks
func ProxyHeartbeatChecks(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/heartbeat/checks"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyRecurringPreview proxies GET /api/recurring/preview
func ProxyRecurringPreview(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/recurring/preview"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyMonitoringLLMUsage proxies GET /api/monitoring/llm/usage
func ProxyMonitoringLLMUsage(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/monitoring/llm/usage"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyMonitoringStats proxies GET /api/monitoring/stats/summary
func ProxyMonitoringStats(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/monitoring/stats/summary"
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyMonitoringEvents proxies GET /api/monitoring/events
func ProxyMonitoringEvents(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/monitoring/events"
		proxyToDuq(w, r, duqURL, false)
	}
}

// =============================================================================
// Conversations Proxy (Duq owns conversation storage)
// =============================================================================

// ProxyConversationsList proxies GET /api/conversations with RBAC
func ProxyConversationsList(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		duqURL := deps.Config.DuqURL + "/api/conversations"
		proxyToDuq(w, r, duqURL, true) // enforce user_id
	}
}

// ProxyConversationsCreate proxies POST /api/conversations with RBAC
func ProxyConversationsCreate(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value("user_id").(int64)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Parse JSON to inject user_id
		var payload map[string]interface{}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}
		} else {
			payload = make(map[string]interface{})
		}

		// Inject user_id
		payload["user_id"] = userID

		modifiedBody, _ := json.Marshal(payload)
		duqURL := deps.Config.DuqURL + "/api/conversations"

		proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", duqURL, strings.NewReader(string(modifiedBody)))
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			log.Printf("[proxy] Conversation create error: %v", err)
			http.Error(w, "Failed to create conversation", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyConversationsUpdate proxies PUT /api/conversations/{id}
func ProxyConversationsUpdate(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conversationID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/conversations/" + conversationID

		body, _ := io.ReadAll(r.Body)
		proxyReq, _ := http.NewRequestWithContext(r.Context(), "PUT", duqURL, strings.NewReader(string(body)))
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := duqProxyClient.Do(proxyReq)
		if err != nil {
			log.Printf("[proxy] Conversation update error: %v", err)
			http.Error(w, "Failed to update conversation", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// ProxyConversationsEnd proxies DELETE /api/conversations/{id}
func ProxyConversationsEnd(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conversationID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/conversations/" + conversationID
		proxyToDuq(w, r, duqURL, false)
	}
}

// ProxyConversationsMessages proxies GET /api/conversations/{id}/messages
func ProxyConversationsMessages(deps *ProxyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conversationID := r.PathValue("id")
		duqURL := deps.Config.DuqURL + "/api/conversations/" + conversationID + "/messages"
		proxyToDuq(w, r, duqURL, false)
	}
}
