package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// handleCallbackQuery processes inline keyboard button clicks
func handleCallbackQuery(w http.ResponseWriter, callback *TelegramCallbackQuery, deps *TelegramDeps) {
	if callback.Message == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	chatID := callback.Message.Chat.ID
	data := callback.Data

	// Answer callback to remove loading state
	AnswerCallbackQuery(deps.Config, callback.ID, "")

	log.Printf("[telegram] Callback from %d: %s", chatID, data)

	switch data {
	case "menu_history":
		handleMenuHistory(w, chatID, deps)
	case "menu_settings":
		handleMenuSettings(w, chatID, deps)
	case "menu_help":
		handleMenuHelp(w, chatID, deps)
	case "menu_back":
		handleMenuBack(w, chatID, deps)
	default:
		log.Printf("[telegram] Unknown callback data: %s", data)
		w.WriteHeader(http.StatusOK)
	}
}

// handleMenuHistory shows last 10 messages
func handleMenuHistory(w http.ResponseWriter, chatID int64, deps *TelegramDeps) {
	if deps.SessionService == nil {
		SendTelegramMessage(deps.Config, chatID, "❌ История недоступна")
		w.WriteHeader(http.StatusOK)
		return
	}

	convID, err := deps.SessionService.GetOrCreateConversationID(chatID)
	if err != nil {
		SendTelegramMessage(deps.Config, chatID, "❌ Не удалось загрузить историю")
		w.WriteHeader(http.StatusOK)
		return
	}

	messages, err := deps.SessionService.GetRecentMessagesSimple(convID, 10)
	if err != nil {
		SendTelegramMessage(deps.Config, chatID, "❌ Не удалось загрузить историю")
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(messages) == 0 {
		SendTelegramMessage(deps.Config, chatID, "📭 История пуста. Отправь мне сообщение!")
		w.WriteHeader(http.StatusOK)
		return
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("📜 *Последние %d сообщений:*\n\n", len(messages)))

	for _, m := range messages {
		emoji := "👤"
		if m.Role == "assistant" {
			emoji = "🤖"
		}
		content := m.Content
		if len(content) > 150 {
			content = content[:150] + "..."
		}
		response.WriteString(fmt.Sprintf("%s %s\n\n", emoji, content))
	}

	SendTelegramMessage(deps.Config, chatID, response.String())
	w.WriteHeader(http.StatusOK)
}

// handleMenuSettings shows user settings
func handleMenuSettings(w http.ResponseWriter, chatID int64, deps *TelegramDeps) {
	if deps.DBClient != nil {
		user, err := deps.DBClient.GetUserByTelegramID(chatID)
		if err != nil || user == nil {
			SendTelegramMessage(deps.Config, chatID, "❌ Настройки недоступны. Отправь /start для регистрации.")
			w.WriteHeader(http.StatusOK)
			return
		}

		settingsText := fmt.Sprintf(`⚙️ *Твои настройки*

👤 *Аккаунт:*
• Имя: %s %s
• Username: %s
• Роль: %s

🌍 *Предпочтения:*
• Часовой пояс: %s
• Язык: %s

Для изменения настроек обратись к администратору.`, user.FirstName, user.LastName, user.Username, user.Role, user.Timezone, user.PreferredLanguage)

		// Settings keyboard with back button
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "« Назад", CallbackData: "menu_back"}},
			},
		}
		SendTelegramMessageWithKeyboard(deps.Config, chatID, settingsText, keyboard)
	} else {
		SendTelegramMessage(deps.Config, chatID, "❌ Настройки недоступны")
	}
	w.WriteHeader(http.StatusOK)
}

// handleMenuHelp shows help message
func handleMenuHelp(w http.ResponseWriter, chatID int64, deps *TelegramDeps) {
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
• Или отправь голосовое — я пойму!

*Команды:*
• /start — главное меню
• /history — история сообщений
• /settings — настройки`

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "« Назад", CallbackData: "menu_back"}},
		},
	}
	SendTelegramMessageWithKeyboard(deps.Config, chatID, helpText, keyboard)
	w.WriteHeader(http.StatusOK)
}

// handleMenuBack returns to main menu
func handleMenuBack(w http.ResponseWriter, chatID int64, deps *TelegramDeps) {
	welcomeText := `🏠 *Главное меню*

Выбери действие или просто отправь мне сообщение!`

	SendTelegramMessageWithKeyboard(deps.Config, chatID, welcomeText, getMainMenuKeyboard())
	w.WriteHeader(http.StatusOK)
}
