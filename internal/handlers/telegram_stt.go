package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"duq-gateway/internal/config"
)

// transcribeVoice downloads a voice file from Telegram and sends to Duq for STT
func transcribeVoice(cfg *config.Config, fileID string) (string, error) {
	botToken := cfg.Telegram.BotToken
	if botToken == "" {
		return "", fmt.Errorf("telegram bot token not configured")
	}

	// Get file path from Telegram
	getFileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", botToken, fileID)
	resp, err := http.Get(getFileURL)
	if err != nil {
		return "", fmt.Errorf("failed to get file info: %w", err)
	}
	defer resp.Body.Close()

	var fileResp TelegramFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return "", fmt.Errorf("failed to decode file response: %w", err)
	}

	if !fileResp.OK || fileResp.Result.FilePath == "" {
		return "", fmt.Errorf("telegram API returned error or empty path")
	}

	// Download the file
	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken, fileResp.Result.FilePath)
	fileResp2, err := http.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer fileResp2.Body.Close()

	// Read audio bytes
	audioBytes, err := io.ReadAll(fileResp2.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read audio: %w", err)
	}

	log.Printf("[telegram] Downloaded audio: %d bytes", len(audioBytes))

	// Send to Duq /api/voice/transcribe for STT
	duqURL := cfg.DuqURL
	if duqURL == "" {
		duqURL = "http://localhost:8081"
	}

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("audio", "voice.ogg")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return "", fmt.Errorf("failed to write audio: %w", err)
	}
	writer.Close()

	// Send request to Duq
	url := duqURL + "/api/voice/transcribe"
	log.Printf("[telegram] POST %s (audio=%d bytes)", url, len(audioBytes))

	client := &http.Client{Timeout: 120 * time.Second}
	transcribeResp, err := client.Post(url, writer.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer transcribeResp.Body.Close()

	body, err := io.ReadAll(transcribeResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if transcribeResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("duq transcribe returned %d: %s", transcribeResp.StatusCode, string(body))
	}

	var duqResp DuqTranscribeResponse
	if err := json.Unmarshal(body, &duqResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if duqResp.Transcription == "" {
		return "", fmt.Errorf("STT returned empty result")
	}

	log.Printf("[telegram] Duq STT result: %s", truncateStr(duqResp.Transcription, 100))
	return duqResp.Transcription, nil
}
