package handlers

import (
	"fmt"
	"log"
	"net/http"
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
// NOTE: History is managed by Duq backend, not gateway
func handleMenuHistory(w http.ResponseWriter, chatID int64, deps *TelegramDeps) {
	SendTelegramMessage(deps.Config, chatID, "📜 История хранится на сервере. Спроси меня: \"покажи историю\"")
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
