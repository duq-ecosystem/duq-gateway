package channels

import (
	"log"
)

// ObsidianChannel handles obsidian output
// Note: Actual saving is done by Duq via MCP tools,
// this channel just notifies the user
type ObsidianChannel struct {
	notifier *TelegramChannel
}

// NewObsidianChannel creates a new obsidian channel
func NewObsidianChannel(notifier *TelegramChannel) *ObsidianChannel {
	return &ObsidianChannel{
		notifier: notifier,
	}
}

func (c *ObsidianChannel) Name() string {
	return "obsidian"
}

func (c *ObsidianChannel) CanHandle(ctx *ResponseContext) bool {
	return true // Obsidian is always available (saving done by Duq)
}

func (c *ObsidianChannel) Send(ctx *ResponseContext) error {
	log.Printf("[obsidian] Response saved to Obsidian (done by Duq)")

	// Notify user via telegram
	if c.notifier != nil {
		return c.notifier.SendTextMessage(ctx.ChatID, "📝 Сохранено в Obsidian")
	}

	return nil
}
