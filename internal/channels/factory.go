package channels

// RouterBuilder simplifies router construction
// Usage:
//
//	router := channels.NewBuilder().
//	    WithTelegram(botToken).
//	    WithEmail().
//	    WithObsidian().
//	    WithSilent().
//	    Build()
type RouterBuilder struct {
	telegram *TelegramChannel
	channels []Channel
}

// NewBuilder creates a new router builder
func NewBuilder() *RouterBuilder {
	return &RouterBuilder{
		channels: make([]Channel, 0),
	}
}

// WithTelegram adds telegram channel (usually first, used as fallback)
// Note: TTS is done by Duq, no local TTS config needed
func (b *RouterBuilder) WithTelegram(botToken string) *RouterBuilder {
	b.telegram = NewTelegramChannel(botToken)
	b.channels = append(b.channels, b.telegram)
	return b
}

// WithEmail adds email channel with gws sender
func (b *RouterBuilder) WithEmail() *RouterBuilder {
	b.channels = append(b.channels, NewEmailChannel(&GWSEmailSender{}, b.telegram))
	return b
}

// WithEmailCustom adds email channel with custom sender (for testing)
func (b *RouterBuilder) WithEmailCustom(sender EmailSender) *RouterBuilder {
	b.channels = append(b.channels, NewEmailChannel(sender, b.telegram))
	return b
}

// WithObsidian adds obsidian channel
func (b *RouterBuilder) WithObsidian() *RouterBuilder {
	b.channels = append(b.channels, NewObsidianChannel(b.telegram))
	return b
}

// WithSilent adds silent channel
func (b *RouterBuilder) WithSilent() *RouterBuilder {
	b.channels = append(b.channels, NewSilentChannel())
	return b
}

// WithCustom adds any custom channel implementation
func (b *RouterBuilder) WithCustom(channel Channel) *RouterBuilder {
	b.channels = append(b.channels, channel)
	return b
}

// Build creates the router with all registered channels
func (b *RouterBuilder) Build() *Router {
	return NewRouter(b.channels, "telegram")
}

// TelegramChannel returns the telegram channel for direct access
func (b *RouterBuilder) TelegramChannel() *TelegramChannel {
	return b.telegram
}
