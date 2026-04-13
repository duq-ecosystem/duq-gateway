package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"duq-gateway/internal/config"
)

// SendTelegramMessage sends a text message to Telegram (exported for use by other handlers)
func SendTelegramMessage(cfg *config.Config, chatID int64, text string) error {
	botToken := cfg.Telegram.BotToken
	if botToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	log.Printf("[telegram] Sent text message to %d", chatID)
	return nil
}

// SendTelegramMessageWithKeyboard sends a message with inline keyboard
func SendTelegramMessageWithKeyboard(cfg *config.Config, chatID int64, text string, keyboard *InlineKeyboardMarkup) error {
	botToken := cfg.Telegram.BotToken
	if botToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "Markdown",
		"reply_markup": keyboard,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	log.Printf("[telegram] Sent message with keyboard to %d", chatID)
	return nil
}

// AnswerCallbackQuery answers a callback query (removes loading state from button)
func AnswerCallbackQuery(cfg *config.Config, callbackID string, text string) error {
	botToken := cfg.Telegram.BotToken
	if botToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", botToken)

	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	if text != "" {
		payload["text"] = text
		payload["show_alert"] = false
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// getMainMenuKeyboard returns the main menu inline keyboard
func getMainMenuKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📜 История", CallbackData: "menu_history"},
				{Text: "⚙️ Настройки", CallbackData: "menu_settings"},
			},
			{
				{Text: "❓ Помощь", CallbackData: "menu_help"},
			},
		},
	}
}

// sendTelegramVoiceWithCaption generates TTS and sends voice note to Telegram
// If caption is provided and <= 1024 chars, it will be attached to the voice message
func sendTelegramVoiceWithCaption(cfg *config.Config, chatID int64, text string, caption string) error {
	botToken := cfg.Telegram.BotToken
	if botToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}

	voice := cfg.Voice.TTSVoice
	if voice == "" {
		voice = "ru-RU-DmitryNeural"
	}

	// Generate OGG voice file using edge-tts + ffmpeg
	tmpMP3, err := os.CreateTemp("", "tts_*.mp3")
	if err != nil {
		return fmt.Errorf("failed to create temp mp3: %w", err)
	}
	tmpMP3.Close()
	defer os.Remove(tmpMP3.Name())

	tmpOGG := strings.TrimSuffix(tmpMP3.Name(), ".mp3") + ".ogg"
	defer os.Remove(tmpOGG)

	// Run edge-tts
	log.Printf("[telegram] Running TTS: edge-tts --voice %s", voice)
	cmd := exec.Command("edge-tts", "--voice", voice, "--text", text, "--write-media", tmpMP3.Name())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("edge-tts failed: %w", err)
	}

	// Convert to OGG opus (Telegram voice note format)
	cmd = exec.Command("ffmpeg", "-y", "-i", tmpMP3.Name(), "-c:a", "libopus", "-b:a", "64k", tmpOGG)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	// Read OGG file
	oggData, err := os.ReadFile(tmpOGG)
	if err != nil {
		return fmt.Errorf("failed to read ogg: %w", err)
	}

	// Send voice note via Telegram API
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendVoice", botToken)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))

	// Add caption if provided
	if caption != "" {
		writer.WriteField("caption", caption)
	}

	part, err := writer.CreateFormFile("voice", "voice.ogg")
	if err != nil {
		return err
	}
	part.Write(oggData)
	writer.Close()

	resp, err := http.Post(url, writer.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	if caption != "" {
		log.Printf("[telegram] Sent voice note with caption to %d (%d bytes)", chatID, len(oggData))
	} else {
		log.Printf("[telegram] Sent voice note to %d (%d bytes)", chatID, len(oggData))
	}
	return nil
}

// formatTelegramUserID formats telegram chat ID to user ID string
func formatTelegramUserID(chatID int64) string {
	// Just the numeric ID to match duq's format
	return fmt.Sprintf("%d", chatID)
}

// formatUserName formats user display name
func formatUserName(user *TelegramUser) string {
	if user == nil {
		return "unknown"
	}
	if user.Username != "" {
		return "@" + user.Username
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name
}

// truncateStr truncates string to maxLen chars
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
