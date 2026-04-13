package session

import (
	"encoding/json"
	"testing"
	"time"
)

// TestMessageStruct tests Message struct serialization
func TestMessageStruct(t *testing.T) {
	msg := Message{
		ID:        1,
		Role:      "user",
		Content:   "Hello, Duq",
		CreatedAt: time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var parsed Message
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if parsed.ID != msg.ID {
		t.Errorf("ID = %d, want %d", parsed.ID, msg.ID)
	}
	if parsed.Role != msg.Role {
		t.Errorf("Role = %s, want %s", parsed.Role, msg.Role)
	}
	if parsed.Content != msg.Content {
		t.Errorf("Content = %s, want %s", parsed.Content, msg.Content)
	}
}

// TestConversationStruct tests Conversation struct serialization
func TestConversationStruct(t *testing.T) {
	conv := Conversation{
		ID:            "550e8400-e29b-41d4-a716-446655440000",
		UserID:        123456789,
		Title:         "Test Conversation",
		StartedAt:     time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC),
		LastMessageAt: time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
		IsActive:      true,
	}

	data, err := json.Marshal(conv)
	if err != nil {
		t.Fatalf("Failed to marshal conversation: %v", err)
	}

	var parsed Conversation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal conversation: %v", err)
	}

	if parsed.ID != conv.ID {
		t.Errorf("ID = %s, want %s", parsed.ID, conv.ID)
	}
	if parsed.UserID != conv.UserID {
		t.Errorf("UserID = %d, want %d", parsed.UserID, conv.UserID)
	}
	if parsed.IsActive != conv.IsActive {
		t.Errorf("IsActive = %v, want %v", parsed.IsActive, conv.IsActive)
	}
}

// TestAudioMetadataStruct tests AudioMetadata struct
func TestAudioMetadataStruct(t *testing.T) {
	meta := AudioMetadata{
		MessageID:  42,
		DurationMs: 5000,
		Waveform:   []float64{0.1, 0.5, 0.8, 0.3, 0.2},
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Failed to marshal audio metadata: %v", err)
	}

	var parsed AudioMetadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal audio metadata: %v", err)
	}

	if parsed.MessageID != meta.MessageID {
		t.Errorf("MessageID = %d, want %d", parsed.MessageID, meta.MessageID)
	}
	if parsed.DurationMs != meta.DurationMs {
		t.Errorf("DurationMs = %d, want %d", parsed.DurationMs, meta.DurationMs)
	}
	if len(parsed.Waveform) != len(meta.Waveform) {
		t.Errorf("Waveform length = %d, want %d", len(parsed.Waveform), len(meta.Waveform))
	}
}

// TestMessageWithToolCalls tests Message with tool_calls JSON
func TestMessageWithToolCalls(t *testing.T) {
	toolCalls := json.RawMessage(`[{"name": "get_weather", "args": {"city": "Almaty"}}]`)

	msg := Message{
		ID:        1,
		Role:      "assistant",
		Content:   "Let me check the weather",
		ToolCalls: toolCalls,
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with tool_calls: %v", err)
	}

	var parsed Message
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if len(parsed.ToolCalls) == 0 {
		t.Error("Expected tool_calls to be preserved")
	}

	// Parse tool calls
	var tools []map[string]interface{}
	if err := json.Unmarshal(parsed.ToolCalls, &tools); err != nil {
		t.Fatalf("Failed to parse tool_calls: %v", err)
	}

	if len(tools) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(tools))
	}
	if tools[0]["name"] != "get_weather" {
		t.Errorf("Tool name = %v, want get_weather", tools[0]["name"])
	}
}

// TestNewService tests service creation
func TestNewService(t *testing.T) {
	// Test that NewService doesn't panic with nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewService panicked: %v", r)
		}
	}()

	service := NewService(nil)
	if service == nil {
		t.Error("NewService returned nil")
	}
}

// TestMessageRoles tests valid message roles
func TestMessageRoles(t *testing.T) {
	validRoles := []string{"user", "assistant", "system"}

	for _, role := range validRoles {
		msg := Message{
			ID:      1,
			Role:    role,
			Content: "Test content",
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("Failed to marshal message with role %s: %v", role, err)
		}

		var parsed Message
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("Failed to unmarshal message with role %s: %v", role, err)
		}

		if parsed.Role != role {
			t.Errorf("Role = %s, want %s", parsed.Role, role)
		}
	}
}

// TestEmptyWaveform tests AudioMetadata with empty waveform
func TestEmptyWaveform(t *testing.T) {
	meta := AudioMetadata{
		MessageID:  1,
		DurationMs: 3000,
		Waveform:   nil,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Failed to marshal audio metadata: %v", err)
	}

	var parsed AudioMetadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal audio metadata: %v", err)
	}

	// Nil waveform should serialize to null and deserialize to nil
	if parsed.Waveform != nil && len(parsed.Waveform) != 0 {
		t.Errorf("Expected nil or empty waveform, got %v", parsed.Waveform)
	}
}

// TestConversationDefaults tests default values for Conversation
func TestConversationDefaults(t *testing.T) {
	conv := Conversation{
		ID:     "test-id",
		UserID: 123,
	}

	// Default values
	if conv.IsActive {
		t.Error("IsActive should default to false")
	}
	if conv.Title != "" {
		t.Errorf("Title should default to empty, got %s", conv.Title)
	}
}
