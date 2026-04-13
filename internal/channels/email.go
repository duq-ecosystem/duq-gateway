package channels

import (
	"fmt"
	"log"
	"os/exec"
)

// EmailSender is an abstraction for sending emails (Dependency Inversion)
type EmailSender interface {
	Send(to, subject, body string) error
}

// GWSEmailSender sends emails via gws CLI
type GWSEmailSender struct{}

func (s *GWSEmailSender) Send(to, subject, body string) error {
	cmd := exec.Command("gws", "gmail", "send",
		"--to", to,
		"--subject", subject,
		"--body", body,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[email] gws send failed: %v, output: %s", err, string(output))
		return fmt.Errorf("gws gmail send failed: %w", err)
	}

	log.Printf("[email] Sent email to %s, subject: %s", to, subject)
	return nil
}

// EmailChannel sends responses via email
type EmailChannel struct {
	sender   EmailSender
	fallback *TelegramChannel
}

// NewEmailChannel creates a new email channel with fallback
func NewEmailChannel(sender EmailSender, fallback *TelegramChannel) *EmailChannel {
	return &EmailChannel{
		sender:   sender,
		fallback: fallback,
	}
}

func (c *EmailChannel) Name() string {
	return "email"
}

func (c *EmailChannel) CanHandle(ctx *ResponseContext) bool {
	return ctx.UserEmail != ""
}

func (c *EmailChannel) Send(ctx *ResponseContext) error {
	if ctx.UserEmail == "" {
		// Fallback to telegram with error message
		if c.fallback != nil {
			c.fallback.SendTextMessage(ctx.ChatID, "❌ Google аккаунт не подключён. Подключи через /connect_google")
			return c.fallback.Send(ctx)
		}
		return fmt.Errorf("no email configured and no fallback available")
	}

	err := c.sender.Send(ctx.UserEmail, "Duq Report", ctx.Response)
	if err != nil {
		// Fallback to telegram with error + response
		if c.fallback != nil {
			c.fallback.SendTextMessage(ctx.ChatID, "❌ Не удалось отправить на email. Ответ:\n\n"+ctx.Response)
		}
		return err
	}

	// Notify user via telegram that email was sent
	if c.fallback != nil {
		c.fallback.SendTextMessage(ctx.ChatID, fmt.Sprintf("📧 Ответ отправлен на %s", ctx.UserEmail))
	}

	return nil
}
