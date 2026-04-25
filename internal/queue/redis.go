package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	DefaultPrefix = "duq"
)

// Task represents a task to be queued (matches Duq RedisTaskData format)
type Task struct {
	ID              string                 `json:"id"`
	UserID          string                 `json:"user_id"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status"`
	Priority        int                    `json:"priority"`
	Payload         map[string]interface{} `json:"payload,omitempty"`
	Name            string                 `json:"name,omitempty"`
	Description     string                 `json:"description,omitempty"`
	CallbackURL     string                 `json:"callback_url,omitempty"`
	ConversationID  string                 `json:"conversation_id,omitempty"`
	RequestMetadata map[string]interface{} `json:"request_metadata,omitempty"`
	CreatedAt       string                 `json:"created_at"`
	QueuedAt        string                 `json:"queued_at,omitempty"`
}

// Client is Redis queue client
type Client struct {
	rdb    *redis.Client
	prefix string
}

// NewClient creates new Redis queue client
// redisTimeoutSec is the timeout for Redis operations in seconds (default: 5)
func NewClient(redisURL string, redisTimeoutSec int) (*Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	rdb := redis.NewClient(opt)

	// Use configured timeout or fallback to 5 seconds
	timeout := time.Duration(redisTimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	log.Printf("[queue] Connected to Redis (timeout: %v)", timeout)
	return &Client{rdb: rdb, prefix: DefaultPrefix}, nil
}

// Push adds task to Redis queue (same format as Duq expects)
func (c *Client) Push(ctx context.Context, task *Task) (string, error) {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if task.CreatedAt == "" {
		task.CreatedAt = now
	}
	task.QueuedAt = now
	if task.Status == "" {
		task.Status = "PENDING"
	}

	// 1. Store task data in hash
	taskKey := fmt.Sprintf("%s:task:%s", c.prefix, task.ID)
	taskData := c.taskToHash(task)
	if err := c.rdb.HSet(ctx, taskKey, taskData).Err(); err != nil {
		return "", fmt.Errorf("failed to store task: %w", err)
	}

	// 2. Add to user's stream (session lane)
	streamKey := fmt.Sprintf("%s:stream:%s", c.prefix, task.UserID)
	streamID, err := c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"task_id": task.ID,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("failed to add to stream: %w", err)
	}

	log.Printf("[queue] Task pushed: id=%s, user=%s, type=%s, stream_id=%s", task.ID, task.UserID, task.Type, streamID)
	return task.ID, nil
}

func (c *Client) taskToHash(task *Task) map[string]interface{} {
	payloadJSON := ""
	if task.Payload != nil {
		if b, err := json.Marshal(task.Payload); err == nil {
			payloadJSON = string(b)
		}
	}

	metadataJSON := ""
	if task.RequestMetadata != nil {
		if b, err := json.Marshal(task.RequestMetadata); err == nil {
			metadataJSON = string(b)
		}
	}

	return map[string]interface{}{
		"id":               task.ID,
		"user_id":          task.UserID,
		"type":             task.Type,
		"status":           task.Status,
		"priority":         task.Priority,
		"payload":          payloadJSON,
		"name":             task.Name,
		"description":      task.Description,
		"callback_url":     task.CallbackURL,
		"conversation_id":  task.ConversationID,
		"request_metadata": metadataJSON,
		"created_at":       task.CreatedAt,
		"queued_at":        task.QueuedAt,
	}
}

// WaitForResponse waits for task response using BRPOP (blocking)
// Returns response payload or error if timeout
func (c *Client) WaitForResponse(ctx context.Context, taskID string, timeout time.Duration) (map[string]interface{}, error) {
	responseKey := fmt.Sprintf("%s:response:%s", c.prefix, taskID)
	readyKey := fmt.Sprintf("%s:ready", responseKey)

	// BRPOP waits for response signal
	result, err := c.rdb.BRPop(ctx, timeout, readyKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("timeout waiting for response")
		}
		return nil, fmt.Errorf("BRPOP failed: %w", err)
	}

	log.Printf("[queue] Got response signal for task %s: %v", taskID, result)

	// Get actual response data
	data, err := c.rdb.Get(ctx, responseKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Cleanup
	c.rdb.Del(ctx, responseKey, readyKey)

	return payload, nil
}

// GetTaskStatus returns task status (non-blocking)
func (c *Client) GetTaskStatus(ctx context.Context, taskID string) (string, error) {
	taskKey := fmt.Sprintf("%s:task:%s", c.prefix, taskID)
	status, err := c.rdb.HGet(ctx, taskKey, "status").Result()
	if err == redis.Nil {
		return "", fmt.Errorf("task not found")
	}
	return status, err
}

// GetTaskResponse returns response if available (non-blocking)
// Returns nil, nil if response not ready yet
func (c *Client) GetTaskResponse(ctx context.Context, taskID string) (map[string]interface{}, error) {
	responseKey := fmt.Sprintf("%s:response:%s", c.prefix, taskID)

	data, err := c.rdb.Get(ctx, responseKey).Result()
	if err == redis.Nil {
		return nil, nil // Not ready yet
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return payload, nil
}

// Close closes Redis connection
func (c *Client) Close() error {
	return c.rdb.Close()
}

// HistoryMessage represents a single message in conversation history
type HistoryMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// GetHistorySessions returns all session IDs (dates) with history for a user
func (c *Client) GetHistorySessions(ctx context.Context, userID string) ([]string, error) {
	pattern := fmt.Sprintf("%s:history:%s:*", c.prefix, userID)
	var sessions []string

	iter := c.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// Extract session_id from key: duq:history:{user_id}:{session_id}
		parts := splitKey(key, ":")
		if len(parts) >= 4 {
			sessionID := parts[3]
			sessions = append(sessions, sessionID)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return sessions, nil
}

// GetHistoryMessages returns all messages for a user/session
func (c *Client) GetHistoryMessages(ctx context.Context, userID, sessionID string) ([]HistoryMessage, error) {
	key := fmt.Sprintf("%s:history:%s:%s", c.prefix, userID, sessionID)

	data, err := c.rdb.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("lrange failed: %w", err)
	}

	var messages []HistoryMessage
	for _, item := range data {
		var msg HistoryMessage
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			log.Printf("[queue] Failed to parse history message: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// splitKey splits a Redis key by separator
func splitKey(key, sep string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == sep[0] {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	parts = append(parts, key[start:])
	return parts
}
