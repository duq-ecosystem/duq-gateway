package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"duq-gateway/internal/registration"
)

// handleTelegramCommand processes Telegram bot commands
func handleTelegramCommand(w http.ResponseWriter, msg *TelegramMessage, command string, deps *TelegramDeps) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	cmd := parts[0]
	telegramID := msg.Chat.ID

	switch cmd {
	case "/history":
		handleHistoryCommand(w, msg, parts, deps)
	case "/start":
		handleStartCommand(w, msg, deps)
	case "/help":
		handleHelpCommand(w, msg, deps)
	case "/settings":
		handleSettingsCommand(w, telegramID, deps)
	default:
		// Unknown command, ignore
		w.WriteHeader(http.StatusOK)
	}
}

// handleHistoryCommand handles /history command
func handleHistoryCommand(w http.ResponseWriter, msg *TelegramMessage, parts []string, deps *TelegramDeps) {
	telegramID := msg.Chat.ID

	if deps.SessionService == nil {
		SendTelegramMessage(deps.Config, telegramID, "❌ History service not available")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Get conversation ID
	convID, err := deps.SessionService.GetOrCreateConversationID(telegramID)
	if err != nil {
		log.Printf("[telegram/history] Failed to get conversation: %v", err)
		SendTelegramMessage(deps.Config, telegramID, "❌ Failed to load conversation")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Get messages count (default 10, max 50)
	count := 10
	if len(parts) > 1 {
		if n, err := fmt.Sscanf(parts[1], "%d", &count); err == nil && n == 1 {
			if count > 50 {
				count = 50
			}
			if count < 1 {
				count = 1
			}
		}
	}

	// Get recent messages
	messages, err := deps.SessionService.GetRecentMessagesSimple(convID, count)
	if err != nil {
		log.Printf("[telegram/history] Failed to get messages: %v", err)
		SendTelegramMessage(deps.Config, telegramID, "❌ Failed to load history")
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(messages) == 0 {
		SendTelegramMessage(deps.Config, telegramID, "📭 No messages in history")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Format history
	var response strings.Builder
	response.WriteString(fmt.Sprintf("📜 *Last %d messages:*\n\n", len(messages)))

	for _, m := range messages {
		emoji := "👤"
		if m.Role == "assistant" {
			emoji = "🤖"
		}

		// Truncate long messages
		content := m.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}

		response.WriteString(fmt.Sprintf("%s *%s:* %s\n\n", emoji, m.Role, content))
	}

	response.WriteString(fmt.Sprintf("_Conversation ID: %s_", convID))

	SendTelegramMessage(deps.Config, telegramID, response.String())
	w.WriteHeader(http.StatusOK)
}

// handleStartCommand handles /start command
func handleStartCommand(w http.ResponseWriter, msg *TelegramMessage, deps *TelegramDeps) {
	telegramID := msg.Chat.ID

	// Check if user exists using RegistrationService (unified API)
	if deps.RegistrationService != nil {
		userExists := deps.RegistrationService.CheckUserExists(telegramID)

		if !userExists {
			// NEW USER: Auto-register via unified Registration API
			username := ""
			firstName := ""
			lastName := ""
			if msg.From != nil {
				username = msg.From.Username
				firstName = msg.From.FirstName
				lastName = msg.From.LastName
			}

			// Use unified registration service
			regReq := &registration.Request{
				Method:     registration.MethodTelegram,
				TelegramID: &telegramID,
				Username:   username,
				FirstName:  firstName,
				LastName:   lastName,
			}

			resp, err := deps.RegistrationService.Register(context.Background(), regReq)
			if err != nil {
				log.Printf("[telegram] Registration service error: %v", err)
				SendTelegramMessage(deps.Config, telegramID, "❌ Ошибка регистрации. Попробуй позже.")
				w.WriteHeader(http.StatusOK)
				return
			}

			// Welcome message for new user with buttons
			welcomeText := fmt.Sprintf(`🎉 *Добро пожаловать в Duq, %s!*

Твой аккаунт создан успешно.

🤖 Я — твой AI-ассистент. Могу помочь с:
• Ответами на вопросы
• Управлением календарём и задачами
• Работой с почтой Gmail
• Поиском в интернете

Просто напиши или отправь голосовое сообщение!`, firstName)

			SendTelegramMessageWithKeyboard(deps.Config, telegramID, welcomeText, getMainMenuKeyboard())
			log.Printf("[telegram] User registered via unified API: user_id=%d, telegram_id=%d, username=%s",
				resp.UserID, telegramID, username)
			w.WriteHeader(http.StatusOK)
			return
		}

		// EXISTING USER: Show main menu with buttons
		user, err := deps.RegistrationService.GetUserByTelegramID(telegramID)
		if err != nil {
			log.Printf("[telegram] Failed to get user: %v", err)
		}

		var welcomeBack string
		if user != nil && user.FirstName != "" {
			welcomeBack = fmt.Sprintf(`👋 *Привет, %s!*

Чем могу помочь?`, user.FirstName)
		} else {
			welcomeBack = `👋 *Привет!*

Чем могу помочь?`
		}

		SendTelegramMessageWithKeyboard(deps.Config, telegramID, welcomeBack, getMainMenuKeyboard())
		w.WriteHeader(http.StatusOK)
		return
	}

	// Fallback if no RegistrationService configured
	SendTelegramMessageWithKeyboard(deps.Config, telegramID, `🤖 *Duq*

Просто напиши мне или отправь голосовое сообщение!`, getMainMenuKeyboard())
	w.WriteHeader(http.StatusOK)
}

// handleHelpCommand handles /help command
func handleHelpCommand(w http.ResponseWriter, msg *TelegramMessage, deps *TelegramDeps) {
	telegramID := msg.Chat.ID

	helpText := `❓ *Помощь*

🤖 Я — *Duq*, твой AI-ассистент.

*Что я умею:*
• Отвечать на вопросы
• Управлять календарём и задачами
• Работать с почтой Gmail
• Искать в интернете
• И многое другое!

*Как общаться:*
• Просто напиши текстовое сообщение
• Или отправь голосовое — я пойму!`

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📜 История", CallbackData: "menu_history"},
				{Text: "⚙️ Настройки", CallbackData: "menu_settings"},
			},
		},
	}
	SendTelegramMessageWithKeyboard(deps.Config, telegramID, helpText, keyboard)
	w.WriteHeader(http.StatusOK)
}

// handleSettingsCommand handles /settings command
func handleSettingsCommand(w http.ResponseWriter, telegramID int64, deps *TelegramDeps) {
	if deps.DBClient != nil {
		user, err := deps.DBClient.GetUserByTelegramID(telegramID)
		if err != nil {
			log.Printf("[telegram] Failed to get user settings: %v", err)
			SendTelegramMessage(deps.Config, telegramID, "❌ Не удалось загрузить настройки")
			w.WriteHeader(http.StatusOK)
			return
		}

		if user == nil {
			SendTelegramMessage(deps.Config, telegramID, "❌ Пользователь не найден. Отправь /start для регистрации.")
			w.WriteHeader(http.StatusOK)
			return
		}

		settingsText := fmt.Sprintf(`⚙️ *Твои настройки*

👤 *Аккаунт:*
• Username: %s
• Имя: %s %s
• Роль: %s

🌍 *Предпочтения:*
• Часовой пояс: %s
• Язык: %s

Для изменения настроек обратись к администратору.`, user.Username, user.FirstName, user.LastName, user.Role, user.Timezone, user.PreferredLanguage)

		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "🏠 Главное меню", CallbackData: "menu_back"}},
			},
		}
		SendTelegramMessageWithKeyboard(deps.Config, telegramID, settingsText, keyboard)
	} else {
		SendTelegramMessage(deps.Config, telegramID, "❌ Настройки недоступны")
	}
	w.WriteHeader(http.StatusOK)
}
