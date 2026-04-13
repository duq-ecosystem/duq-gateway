package handlers

// Telegram Update structures
type TelegramUpdate struct {
	UpdateID int              `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
}

type TelegramMessage struct {
	MessageID int               `json:"message_id"`
	From      *TelegramUser     `json:"from,omitempty"`
	Chat      *TelegramChat     `json:"chat"`
	Text      string            `json:"text,omitempty"`
	Voice     *TelegramVoice    `json:"voice,omitempty"`
	Audio     *TelegramAudio    `json:"audio,omitempty"`
	Photo     []TelegramPhoto   `json:"photo,omitempty"`
	Document  *TelegramDocument `json:"document,omitempty"`
}

type TelegramUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type TelegramChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

type TelegramVoice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

type TelegramAudio struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
	Title    string `json:"title,omitempty"`
}

type TelegramPhoto struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size,omitempty"`
}

type TelegramDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int    `json:"file_size,omitempty"`
}

type TelegramFileResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		FileID   string `json:"file_id"`
		FilePath string `json:"file_path"`
	} `json:"result"`
}

// Inline Keyboard types
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// Callback Query types
type TelegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    *TelegramUser    `json:"from"`
	Message *TelegramMessage `json:"message,omitempty"`
	Data    string           `json:"data,omitempty"`
}

// Extended Update with callback query support
type TelegramUpdateFull struct {
	UpdateID      int                    `json:"update_id"`
	Message       *TelegramMessage       `json:"message,omitempty"`
	CallbackQuery *TelegramCallbackQuery `json:"callback_query,omitempty"`
}

// HistoryMessage represents a message in conversation history
type HistoryMessage struct {
	Role    string
	Content string
}

// UserPreferences represents user preferences
type UserPreferences struct {
	Timezone          string
	PreferredLanguage string
}

// chatOptions holds options for processing chat messages
type chatOptions struct {
	AllowedTools    []string
	ConversationID  string
	History         []HistoryMessage
	UserPreferences *UserPreferences
	GWSCredentials  map[string]string
}

// TelegramSendRequest is the request body for /api/telegram/send
type TelegramSendRequest struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
	Voice  bool   `json:"voice"` // If true, also send TTS voice note
}

// DuqTranscribeResponse represents Duq /api/voice/transcribe response
type DuqTranscribeResponse struct {
	Transcription string `json:"transcription"`
	Success       bool   `json:"success"`
}
