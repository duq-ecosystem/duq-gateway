package duq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"duq-gateway/internal/config"
	"duq-tracing/tracing"
)

// Client communicates with Duq via HTTP API
type Client struct {
	baseURL      string
	httpClient   *http.Client
	queueTimeout time.Duration // Timeout for queue requests (shorter than chat)
	defaultUser  string
}

// HistoryMessage represents a message in conversation history
type HistoryMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// UserPreferences holds user-specific settings passed from Gateway to Duq
type UserPreferences struct {
	Timezone          string `json:"timezone"`           // IANA timezone, e.g., "Asia/Almaty"
	PreferredLanguage string `json:"preferred_language"` // Language code, e.g., "ru", "en"
}

// ChatRequest represents the request body for /api/chat
type ChatRequest struct {
	Message        string            `json:"message"`
	UserID         *string           `json:"user_id,omitempty"`
	Role           string            `json:"role"`
	AllowedTools   []string          `json:"allowed_tools,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
	History        []HistoryMessage  `json:"history,omitempty"`
	GWSCredentials map[string]string `json:"gws_credentials,omitempty"`

	// User preferences from Gateway (source of truth)
	UserPreferences *UserPreferences `json:"user_preferences,omitempty"`

	// Voice-aware fields
	InputType             string `json:"input_type,omitempty"`              // "text" or "voice"
	SupportsVoiceResponse bool   `json:"supports_voice_response,omitempty"` // Whether client can receive voice
	PreferredVoice        string `json:"preferred_voice,omitempty"`         // TTS voice preference
	PreferredVoiceFormat  string `json:"preferred_voice_format,omitempty"`  // "ogg" for Telegram, "mp3" for mobile
}

// ChatResponse represents the response from /api/chat
type ChatResponse struct {
	Response      string  `json:"response"`
	UserID        string  `json:"user_id"`
	OutputChannel *string `json:"output_channel,omitempty"`

	// Voice-aware fields
	OutputType    string  `json:"output_type,omitempty"`    // "text", "voice", or "both"
	VoicePriority string  `json:"voice_priority,omitempty"` // "high", "normal", or "skip"
	VoiceData     *string `json:"voice_data,omitempty"`     // Base64-encoded audio (MP3)
	VoiceFormat   string  `json:"voice_format,omitempty"`   // Audio format (default: "mp3")
}

// ChatOptions contains optional parameters for chat request
type ChatOptions struct {
	AllowedTools   []string
	ConversationID string
	History        []HistoryMessage
	GWSCredentials map[string]string

	// User preferences from Gateway (source of truth)
	UserPreferences *UserPreferences

	// Voice-aware options
	InputType             string // "text" or "voice"
	SupportsVoiceResponse bool   // Whether client can receive voice
	PreferredVoice        string // TTS voice preference
	PreferredVoiceFormat  string // "ogg" for Telegram, "mp3" for mobile
}

// ==================== Phase 3: Two-Level Queuing ====================

// QueueRequest represents the request body for /api/queue
type QueueRequest struct {
	Message         string                 `json:"message"`
	UserID          string                 `json:"user_id"`
	TaskType        string                 `json:"task_type"`
	Priority        int                    `json:"priority"`
	OutputChannel   string                 `json:"output_channel,omitempty"`
	CallbackURL     string                 `json:"callback_url,omitempty"`
	ConversationID  string                 `json:"conversation_id,omitempty"`
	RequestMetadata map[string]interface{} `json:"request_metadata,omitempty"`
	AllowedTools    []string               `json:"allowed_tools,omitempty"`
	History         []HistoryMessage       `json:"history,omitempty"`
	UserPreferences *UserPreferences       `json:"user_preferences,omitempty"`
}

// QueueResponse represents the response from /api/queue
type QueueResponse struct {
	TaskID            string `json:"task_id"`
	Status            string `json:"status"`
	EstimatedPosition *int   `json:"estimated_position,omitempty"`
}

// CallbackPayload is the payload that Duq sends back to callback_url
type CallbackPayload struct {
	TaskID          string                 `json:"task_id"`
	UserID          string                 `json:"user_id"`
	Success         bool                   `json:"success"`
	Result          map[string]interface{} `json:"result,omitempty"`
	Error           string                 `json:"error,omitempty"`
	ExecutionTimeMs *int                   `json:"execution_time_ms,omitempty"`
	RequestMetadata map[string]interface{} `json:"request_metadata,omitempty"`
}

// QueueOptions contains optional parameters for queue request
type QueueOptions struct {
	TaskType        string
	Priority        int
	OutputChannel   string
	CallbackURL     string
	ConversationID  string
	RequestMetadata map[string]interface{}
	AllowedTools    []string
	History         []HistoryMessage
	UserPreferences *UserPreferences
}

// NewClient creates a new Duq HTTP client
func NewClient(cfg *config.Config) *Client {
	baseURL := cfg.DuqURL
	if baseURL == "" {
		baseURL = "http://localhost:8081"
	}

	// Use configured timeout, fallback to 120s
	timeout := time.Duration(cfg.Timeouts.DuqTimeout) * time.Second
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	// Queue timeout (shorter, for task creation only)
	queueTimeout := time.Duration(cfg.Timeouts.QueueTimeout) * time.Second
	if queueTimeout == 0 {
		queueTimeout = 10 * time.Second
	}

	return &Client{
		baseURL:      baseURL,
		defaultUser:  cfg.TelegramChatID,
		queueTimeout: queueTimeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// SendMessage sends a message to the default user (with delivery via Telegram)
func (c *Client) SendMessage(ctx context.Context, message string) error {
	_, err := c.Send(ctx, message, c.defaultUser, nil)
	return err
}

// Send sends a message to a specific user and returns the response
func (c *Client) Send(ctx context.Context, message, userID string, opts *ChatOptions) (string, error) {
	resp, err := c.chat(ctx, message, userID, opts)
	if err != nil {
		return "", err
	}
	return resp.Response, nil
}

// SendWithoutDeliver sends a message without delivery (returns response only)
// Deprecated: use Send with options instead
func (c *Client) SendWithoutDeliver(ctx context.Context, message, userID string) (string, error) {
	resp, err := c.chat(ctx, message, userID, nil)
	if err != nil {
		return "", err
	}
	return resp.Response, nil
}

// SendWithOptions sends a message with full options (allowed_tools, history, etc.)
// Returns response text only. Use SendFull for output_channel routing.
func (c *Client) SendWithOptions(ctx context.Context, message, userID string, opts ChatOptions) (string, error) {
	resp, err := c.chat(ctx, message, userID, &opts)
	if err != nil {
		return "", err
	}
	return resp.Response, nil
}

// SendFull sends a message and returns full ChatResponse including output_channel
func (c *Client) SendFull(ctx context.Context, message, userID string, opts ChatOptions) (*ChatResponse, error) {
	return c.chat(ctx, message, userID, &opts)
}

// chat is the internal method that calls duq /api/chat
func (c *Client) chat(ctx context.Context, message, userID string, opts *ChatOptions) (*ChatResponse, error) {
	req := ChatRequest{
		Message: message,
		UserID:  &userID,
		Role:    "user", // We no longer use role-based MCP server selection
	}

	// Apply options if provided
	if opts != nil {
		req.AllowedTools = opts.AllowedTools
		req.ConversationID = opts.ConversationID
		req.History = opts.History
		req.GWSCredentials = opts.GWSCredentials
		req.UserPreferences = opts.UserPreferences

		// Voice-aware fields
		req.InputType = opts.InputType
		req.SupportsVoiceResponse = opts.SupportsVoiceResponse
		req.PreferredVoice = opts.PreferredVoice
		req.PreferredVoiceFormat = opts.PreferredVoiceFormat
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/api/chat"
	traceID := tracing.GetTraceID(ctx)
	log.Printf("[duq] POST %s user=%s tools=%d history=%d message=%d chars [trace:%s]",
		url, userID, len(req.AllowedTools), len(req.History), len(message), traceID[:8])

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Propagate trace headers to backend
	if traceID != "" {
		httpReq.Header.Set(tracing.TraceIDHeader, traceID)
	}
	if spanID := tracing.GetSpanID(ctx); spanID != "" {
		httpReq.Header.Set(tracing.SpanIDHeader, spanID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[duq] Error response: %d %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("duq returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	channel := "telegram"
	if chatResp.OutputChannel != nil {
		channel = *chatResp.OutputChannel
	}
	log.Printf("[duq] Success, response=%d chars, channel=%s", len(chatResp.Response), channel)
	return &chatResp, nil
}

// ==================== Phase 3: Queue Methods ====================

// QueueAsync sends a message to the queue for async processing.
// Returns immediately with task_id. Result delivered via callback_url.
func (c *Client) QueueAsync(ctx context.Context, message, userID string, opts QueueOptions) (*QueueResponse, error) {
	req := QueueRequest{
		Message:         message,
		UserID:          userID,
		TaskType:        opts.TaskType,
		Priority:        opts.Priority,
		OutputChannel:   opts.OutputChannel,
		CallbackURL:     opts.CallbackURL,
		ConversationID:  opts.ConversationID,
		RequestMetadata: opts.RequestMetadata,
		AllowedTools:    opts.AllowedTools,
		History:         opts.History,
		UserPreferences: opts.UserPreferences,
	}

	// Set defaults
	if req.TaskType == "" {
		req.TaskType = "message"
	}
	if req.Priority == 0 {
		req.Priority = 50
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/api/queue"
	traceID := tracing.GetTraceID(ctx)
	log.Printf("[duq] POST %s (async) user=%s task_type=%s priority=%d callback=%v [trace:%s]",
		url, userID, req.TaskType, req.Priority, req.CallbackURL != "", traceID[:8])

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Propagate trace headers to backend
	if traceID != "" {
		httpReq.Header.Set(tracing.TraceIDHeader, traceID)
	}
	if spanID := tracing.GetSpanID(ctx); spanID != "" {
		httpReq.Header.Set(tracing.SpanIDHeader, spanID)
	}

	// Short timeout for queue request (task creation only)
	client := &http.Client{Timeout: c.queueTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[duq] Queue error response: %d %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("duq queue returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var queueResp QueueResponse
	if err := json.Unmarshal(respBody, &queueResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("[duq] Task queued: id=%s, status=%s", queueResp.TaskID, queueResp.Status)
	return &queueResp, nil
}

// GetCallbackURL returns the callback URL for this gateway instance
func (c *Client) GetCallbackURL(gatewayHost string) string {
	return fmt.Sprintf("http://%s/api/duq/callback", gatewayHost)
}
