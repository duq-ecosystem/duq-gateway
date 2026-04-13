package channels

import (
	"fmt"
	"log"
)

// ResponseContext contains all info needed to route a response
type ResponseContext struct {
	ChatID    int64
	UserEmail string
	Response  string
	IsVoice   bool

	// Voice-aware fields (from Duq response)
	OutputType    string // "text", "voice", or "both"
	VoicePriority string // "high", "normal", or "skip"
	VoiceData     []byte // Audio bytes (MP3 from Duq)
	VoiceFormat   string // Audio format (default: "mp3")
}

// Channel is the interface for output channels (Open/Closed, Dependency Inversion)
type Channel interface {
	// Name returns channel identifier
	Name() string
	// Send delivers response to this channel
	Send(ctx *ResponseContext) error
	// CanHandle returns true if this channel can handle the context
	CanHandle(ctx *ResponseContext) bool
}

// Router routes responses to appropriate channels
type Router struct {
	channels       map[string]Channel
	defaultChannel Channel
	fallback       Channel
}

// NewRouter creates a router with registered channels
func NewRouter(channels []Channel, defaultName string) *Router {
	r := &Router{
		channels: make(map[string]Channel),
	}

	for _, ch := range channels {
		r.channels[ch.Name()] = ch
		if ch.Name() == defaultName {
			r.defaultChannel = ch
		}
		if ch.Name() == "telegram" {
			r.fallback = ch
		}
	}

	// If no default set, use telegram
	if r.defaultChannel == nil && r.fallback != nil {
		r.defaultChannel = r.fallback
	}

	return r
}

// Route sends response to the specified channel
// Gateway is a dumb executor - agent decides everything via tools (gmail_send, etc.)
// Gateway just delivers agent's text response to Telegram
func (r *Router) Route(channelName string, ctx *ResponseContext) error {
	// Default to telegram - agent uses gmail_send directly if wants email
	if channelName == "" {
		channelName = "telegram"
	}

	ch, ok := r.channels[channelName]
	if !ok {
		log.Printf("[router] Unknown channel '%s', using default", channelName)
		ch = r.defaultChannel
	}

	if ch == nil {
		return fmt.Errorf("no channel available")
	}

	// Check if channel can handle this context
	if !ch.CanHandle(ctx) {
		log.Printf("[router] Channel '%s' cannot handle context, falling back", ch.Name())
		if r.fallback != nil && r.fallback.CanHandle(ctx) {
			return r.routeWithFallbackNotice(ch, r.fallback, ctx)
		}
		return fmt.Errorf("channel '%s' cannot handle context and no fallback available", ch.Name())
	}

	return ch.Send(ctx)
}

// routeWithFallbackNotice sends error to fallback and then original response
func (r *Router) routeWithFallbackNotice(failed, fallback Channel, ctx *ResponseContext) error {
	// This is handled by individual channels now
	return fallback.Send(ctx)
}
