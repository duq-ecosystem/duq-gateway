package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"duq-gateway/internal/config"
	"duq-gateway/internal/credentials"
	"duq-gateway/internal/queue"
	"duq-gateway/internal/registration"
	"duq-gateway/internal/session"
)

// ==================== Utility Function Tests ====================

func TestFormatTelegramUserID(t *testing.T) {
	tests := []struct {
		name   string
		chatID int64
		want   string
	}{
		{"positive ID", 12345, "12345"},
		{"large ID", 1234567890123, "1234567890123"},
		{"zero", 0, "0"},
		{"negative ID", -12345, "-12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTelegramUserID(tt.chatID)
			if got != tt.want {
				t.Errorf("formatTelegramUserID(%d) = %s, want %s", tt.chatID, got, tt.want)
			}
		})
	}
}

func TestFormatUserName(t *testing.T) {
	tests := []struct {
		name string
		user *TelegramUser
		want string
	}{
		{
			name: "nil user",
			user: nil,
			want: "unknown",
		},
		{
			name: "with username",
			user: &TelegramUser{Username: "testuser"},
			want: "@testuser",
		},
		{
			name: "first name only",
			user: &TelegramUser{FirstName: "John"},
			want: "John",
		},
		{
			name: "full name",
			user: &TelegramUser{FirstName: "John", LastName: "Doe"},
			want: "John Doe",
		},
		{
			name: "username takes precedence",
			user: &TelegramUser{Username: "johnd", FirstName: "John", LastName: "Doe"},
			want: "@johnd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUserName(tt.user)
			if got != tt.want {
				t.Errorf("formatUserName() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"zero length", "hello", 0, "..."},
		{"unicode", "привет мир", 12, "привет..."},  // Cyrillic is 2 bytes per char
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ==================== Keyboard Tests ====================

func TestGetMainMenuKeyboard(t *testing.T) {
	keyboard := getMainMenuKeyboard()

	if keyboard == nil {
		t.Fatal("getMainMenuKeyboard() returned nil")
	}

	// Check structure
	if len(keyboard.InlineKeyboard) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(keyboard.InlineKeyboard))
	}

	// First row should have 2 buttons
	if len(keyboard.InlineKeyboard[0]) != 2 {
		t.Errorf("First row should have 2 buttons, got %d", len(keyboard.InlineKeyboard[0]))
	}

	// Second row should have 1 button
	if len(keyboard.InlineKeyboard[1]) != 1 {
		t.Errorf("Second row should have 1 button, got %d", len(keyboard.InlineKeyboard[1]))
	}

	// Check button callbacks
	if keyboard.InlineKeyboard[0][0].CallbackData != "menu_history" {
		t.Errorf("First button callback = %s, want menu_history", keyboard.InlineKeyboard[0][0].CallbackData)
	}
	if keyboard.InlineKeyboard[0][1].CallbackData != "menu_settings" {
		t.Errorf("Second button callback = %s, want menu_settings", keyboard.InlineKeyboard[0][1].CallbackData)
	}
	if keyboard.InlineKeyboard[1][0].CallbackData != "menu_help" {
		t.Errorf("Third button callback = %s, want menu_help", keyboard.InlineKeyboard[1][0].CallbackData)
	}
}

// ==================== Struct Serialization Tests ====================

func TestTelegramUpdateSerialization(t *testing.T) {
	update := TelegramUpdate{
		UpdateID: 12345,
		Message: &TelegramMessage{
			MessageID: 1,
			Text:      "Hello",
			Chat: &TelegramChat{
				ID:   98765,
				Type: "private",
			},
		},
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded TelegramUpdate
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.UpdateID != 12345 {
		t.Errorf("UpdateID = %d, want 12345", decoded.UpdateID)
	}
	if decoded.Message.Text != "Hello" {
		t.Errorf("Text = %s, want Hello", decoded.Message.Text)
	}
}

func TestTelegramVoiceSerialization(t *testing.T) {
	voice := TelegramVoice{
		FileID:       "abc123",
		FileUniqueID: "unique123",
		Duration:     10,
		MimeType:     "audio/ogg",
		FileSize:     5000,
	}

	data, err := json.Marshal(voice)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"file_id":"abc123"`) {
		t.Errorf("JSON missing file_id: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"duration":10`) {
		t.Errorf("JSON missing duration: %s", jsonStr)
	}
}

func TestInlineKeyboardMarkupSerialization(t *testing.T) {
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Button 1", CallbackData: "callback1"},
				{Text: "Button 2", URL: "https://example.com"},
			},
		},
	}

	data, err := json.Marshal(keyboard)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"text":"Button 1"`) {
		t.Errorf("JSON missing button text: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"callback_data":"callback1"`) {
		t.Errorf("JSON missing callback_data: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"url":"https://example.com"`) {
		t.Errorf("JSON missing url: %s", jsonStr)
	}
}

// ==================== Mock Implementations ====================

type MockRBACService struct {
	allowedTools []string
	ensureErr    error
	toolsErr     error
}

func (m *MockRBACService) GetAllowedTools(userID int64) ([]string, error) {
	return m.allowedTools, m.toolsErr
}

func (m *MockRBACService) EnsureUser(userID int64, username, firstName, lastName string) error {
	return m.ensureErr
}

type MockSessionService struct {
	conversationID string
	messages       []session.HistoryMessage
	convErr        error
	msgErr         error
	saveErr        error
}

func (m *MockSessionService) GetOrCreateConversationID(userID int64) (string, error) {
	return m.conversationID, m.convErr
}

func (m *MockSessionService) GetRecentMessagesSimple(conversationID string, limit int) ([]session.HistoryMessage, error) {
	return m.messages, m.msgErr
}

func (m *MockSessionService) SaveMessageSimple(conversationID string, role, content string) error {
	return m.saveErr
}

type MockCredService struct {
	creds   *credentials.UserCredentials
	getErr  error
	saveErr error
}

func (m *MockCredService) GetCredentials(userID int64, provider string) (*credentials.UserCredentials, error) {
	return m.creds, m.getErr
}

func (m *MockCredService) SaveCredentials(creds *credentials.UserCredentials) error {
	return m.saveErr
}

type MockQueueClient struct {
	pushedTasks []*queue.Task
	pushErr     error
}

func (m *MockQueueClient) Push(ctx interface{}, task *queue.Task) (string, error) {
	m.pushedTasks = append(m.pushedTasks, task)
	return "task-id-123", m.pushErr
}

type MockRegistrationService struct {
	userExists bool
	user       *registration.User
	getErr     error
	regResp    *registration.Response
	regErr     error
}

func (m *MockRegistrationService) CheckUserExists(telegramID int64) bool {
	return m.userExists
}

func (m *MockRegistrationService) GetUserByTelegramID(telegramID int64) (*registration.User, error) {
	return m.user, m.getErr
}

func (m *MockRegistrationService) Register(ctx interface{}, req *registration.Request) (*registration.Response, error) {
	return m.regResp, m.regErr
}

// ==================== Handler Tests ====================

func TestTelegramWithDeps_InvalidJSON(t *testing.T) {
	deps := &TelegramDeps{
		Config: &config.Config{},
	}

	handler := TelegramWithDeps(deps)

	req := httptest.NewRequest("POST", "/api/telegram/webhook", strings.NewReader("invalid json"))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestTelegramWithDeps_NoMessage(t *testing.T) {
	deps := &TelegramDeps{
		Config: &config.Config{},
	}

	handler := TelegramWithDeps(deps)

	update := TelegramUpdateFull{
		UpdateID: 12345,
		Message:  nil,
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest("POST", "/api/telegram/webhook", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestTelegramWithDeps_BotMessage(t *testing.T) {
	deps := &TelegramDeps{
		Config: &config.Config{},
	}

	handler := TelegramWithDeps(deps)

	update := TelegramUpdateFull{
		UpdateID: 12345,
		Message: &TelegramMessage{
			MessageID: 1,
			From:      &TelegramUser{ID: 123, IsBot: true},
			Chat:      &TelegramChat{ID: 456, Type: "private"},
			Text:      "Bot message",
		},
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest("POST", "/api/telegram/webhook", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should return OK but not process bot messages
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// ==================== TelegramSend Tests ====================

func TestTelegramSend_InvalidJSON(t *testing.T) {
	cfg := &config.Config{}
	handler := TelegramSend(cfg)

	req := httptest.NewRequest("POST", "/api/telegram/send", strings.NewReader("invalid"))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestTelegramSend_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		body TelegramSendRequest
	}{
		{"missing chat_id", TelegramSendRequest{ChatID: 0, Text: "hello"}},
		{"missing text", TelegramSendRequest{ChatID: 123, Text: ""}},
		{"both missing", TelegramSendRequest{ChatID: 0, Text: ""}},
	}

	cfg := &config.Config{}
	handler := TelegramSend(cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/api/telegram/send", bytes.NewReader(body))
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
			}
		})
	}
}

// ==================== TelegramSendRequest Tests ====================

func TestTelegramSendRequestSerialization(t *testing.T) {
	req := TelegramSendRequest{
		ChatID: 12345,
		Text:   "Hello world",
		Voice:  true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded TelegramSendRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ChatID != 12345 {
		t.Errorf("ChatID = %d, want 12345", decoded.ChatID)
	}
	if decoded.Text != "Hello world" {
		t.Errorf("Text = %s, want 'Hello world'", decoded.Text)
	}
	if !decoded.Voice {
		t.Error("Voice should be true")
	}
}

// ==================== TelegramFileResponse Tests ====================

func TestTelegramFileResponseDeserialization(t *testing.T) {
	jsonStr := `{
		"ok": true,
		"result": {
			"file_id": "abc123",
			"file_path": "voice/file_1.ogg"
		}
	}`

	var resp TelegramFileResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if !resp.OK {
		t.Error("OK should be true")
	}
	if resp.Result.FileID != "abc123" {
		t.Errorf("FileID = %s, want abc123", resp.Result.FileID)
	}
	if resp.Result.FilePath != "voice/file_1.ogg" {
		t.Errorf("FilePath = %s, want voice/file_1.ogg", resp.Result.FilePath)
	}
}

// ==================== ChatOptions Tests ====================

func TestChatOptionsStruct(t *testing.T) {
	opts := chatOptions{
		AllowedTools:   []string{"calendar", "gmail"},
		ConversationID: "conv-123",
		History: []HistoryMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
		UserPreferences: &UserPreferences{
			Timezone:          "UTC",
			PreferredLanguage: "ru",
		},
		GWSCredentials: map[string]string{
			"access_token": "token123",
		},
	}

	if len(opts.AllowedTools) != 2 {
		t.Errorf("AllowedTools length = %d, want 2", len(opts.AllowedTools))
	}
	if opts.ConversationID != "conv-123" {
		t.Errorf("ConversationID = %s, want conv-123", opts.ConversationID)
	}
	if len(opts.History) != 2 {
		t.Errorf("History length = %d, want 2", len(opts.History))
	}
	if opts.UserPreferences.Timezone != "UTC" {
		t.Errorf("Timezone = %s, want UTC", opts.UserPreferences.Timezone)
	}
}

// ==================== Edge Cases ====================

func TestTelegramUpdateFull_CallbackQuery(t *testing.T) {
	update := TelegramUpdateFull{
		UpdateID: 12345,
		CallbackQuery: &TelegramCallbackQuery{
			ID:   "callback123",
			From: &TelegramUser{ID: 456, FirstName: "John"},
			Data: "menu_history",
		},
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded TelegramUpdateFull
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.CallbackQuery == nil {
		t.Fatal("CallbackQuery should not be nil")
	}
	if decoded.CallbackQuery.Data != "menu_history" {
		t.Errorf("Data = %s, want menu_history", decoded.CallbackQuery.Data)
	}
}

func TestTelegramDocument(t *testing.T) {
	doc := TelegramDocument{
		FileID:   "doc123",
		FileName: "document.pdf",
		MimeType: "application/pdf",
		FileSize: 102400,
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"file_name":"document.pdf"`) {
		t.Errorf("JSON missing file_name: %s", jsonStr)
	}
}

func TestTelegramPhoto(t *testing.T) {
	photo := TelegramPhoto{
		FileID:   "photo123",
		Width:    1920,
		Height:   1080,
		FileSize: 500000,
	}

	data, err := json.Marshal(photo)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"width":1920`) {
		t.Errorf("JSON missing width: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"height":1080`) {
		t.Errorf("JSON missing height: %s", jsonStr)
	}
}

// ==================== Response Body Tests ====================

func TestTelegramSend_ValidResponse(t *testing.T) {
	// Create a mock Telegram API server
	mockTelegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer mockTelegram.Close()

	// We can't easily test SendTelegramMessage without mocking the HTTP client
	// So we just test the request structure validation
	cfg := &config.Config{}
	handler := TelegramSend(cfg)

	// Valid request but no bot token configured
	reqBody := TelegramSendRequest{
		ChatID: 123456,
		Text:   "Test message",
		Voice:  false,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/telegram/send", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should fail because bot token is not configured
	if rr.Code == http.StatusOK {
		// Reading response body
		respBody, _ := io.ReadAll(rr.Body)
		t.Logf("Response: %s", string(respBody))
	}
}
