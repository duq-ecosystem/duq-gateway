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

// Close closes Redis connection
func (c *Client) Close() error {
	return c.rdb.Close()
}
