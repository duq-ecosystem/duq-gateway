# Duq Webhook Gateway

HTTP gateway для приёма webhooks и голосового API. Отправляет уведомления через Duq.

## Endpoints

| Method | Path | Auth | Описание |
|--------|------|------|----------|
| GET | /health | - | Healthcheck |
| GET | /docs | BasicAuth | Документация (Obsidian → HTML) |
| GET | /docs/{name} | BasicAuth | Конкретный документ |
| POST | /api/telegram/webhook | - | Telegram bot (text + voice + commands) |
| POST | /api/calendar | Token | Google Calendar events |
| POST | /api/gmail | Token | Gmail notifications |
| POST | /api/github | HMAC | GitHub webhooks |
| POST | /api/auth/qr/generate | Token | QR для мобилки |
| POST | /api/auth/qr/verify | - | Верификация QR |
| POST | /api/voice | Mobile Token | Голосовой endpoint (STT → Agent → TTS) |
| GET | /api/conversations | Mobile Token | Список разговоров пользователя |
| GET | /api/conversations/{id}/messages | Mobile Token | История сообщений разговора |
| POST | /api/conversations | Mobile Token | Создать новый разговор |
| PUT | /api/conversations/{id} | Mobile Token | Обновить разговор (title) |
| DELETE | /api/conversations/{id} | Mobile Token | Завершить разговор |

## Сборка

```bash
go build -o duq-gateway .
```

## Конфигурация

Создайте `config.json` из примера:

```bash
cp config.example.json config.json
```

Переменные окружения:
- `DUQ_PORT` - порт (default: 8082)
- `DUQ_TELEGRAM_CHAT_ID` - Telegram chat ID
- `DUQ_URL` - URL Duq API (default: http://localhost:8081)
- `DUQ_TOKEN_CALENDAR` - токен для calendar webhook
- `DUQ_TOKEN_GMAIL` - токен для gmail webhook
- `DUQ_TOKEN_GITHUB` - токен для github webhook

## Запуск

```bash
./duq-gateway
```

## API

### Calendar Webhook

```bash
curl -X POST https://on-za-menya.online/api/calendar \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "reminder",
    "event": {
      "title": "Meeting",
      "start_time": "2026-03-09T10:00:00Z",
      "meet_link": "https://meet.google.com/xxx"
    },
    "minutes_before": 15
  }'
```

### Voice API

```bash
# Send voice command (WAV audio)
curl -X POST https://on-za-menya.online/api/voice \
  -H "Authorization: Bearer mob_xxx" \
  -F "audio=@command.wav"

# Response:
# {
#   "text": "Understood, sir.",
#   "audio": "base64_encoded_ogg_audio"
# }
```

**Features:**
- Saves user message and assistant response to PostgreSQL
- Automatically sends messages to Telegram (cross-channel sync)
- Uses 20 message history + Cortex vector memory for context

### Conversations API

```bash
# Get user's conversations
curl -X GET https://on-za-menya.online/api/conversations \
  -H "Authorization: Bearer mob_xxx"

# Get messages from conversation
curl -X GET "https://on-za-menya.online/api/conversations/{id}/messages?limit=50" \
  -H "Authorization: Bearer mob_xxx"

# Create new conversation
curl -X POST https://on-za-menya.online/api/conversations \
  -H "Authorization: Bearer mob_xxx" \
  -H "Content-Type: application/json" \
  -d '{"title": "My Conversation"}'
```

### Telegram Bot Commands

```
/history       # Show last 10 messages
/history 20    # Show last 20 messages (max 50)
/help          # Show available commands
```

## Systemd

```bash
sudo cp duq-gateway.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable duq-gateway
sudo systemctl start duq-gateway
```

## Authentication

### BasicAuth (для /docs)

Защита документации логином и паролем:

```json
{
  "basic_auth": {
    "username": "user",
    "password": "secret"
  }
}
```

Доступ: `curl -u user:secret https://example.com/docs`

### Token Auth (для /api/*)

Webhook endpoints требуют токен в заголовке:

```
Authorization: Bearer YOUR_TOKEN
```

## Documentation

Endpoint `/docs` отдаёт документацию из Obsidian vault в HTML формате.
Файлы читаются динамически при каждом запросе.
Защищён BasicAuth.

## Mobile ↔ Telegram Sync

Voice commands from mobile app automatically appear in Telegram:

```
Mobile Voice Command
       ↓
POST /api/voice
       ↓
PostgreSQL (conversations + messages)
       ↓
┌──────┴──────────┐
▼                 ▼
Telegram Bot   Mobile App
(auto-push)    (pull-on-focus)
```

**Auto-sync format:**
- User message: `📱 *[Mobile App]*\n\n{command}`
- Assistant response: `🤖 *[Duq]*\n\n{response}`

**Implementation:**
- `voice.go`: Goroutines send messages to Telegram after saving to DB
- `telegram.go`: `/history` command to view conversation history
- `session/service.go`: PostgreSQL-backed session management

## Связанные системы

- **Duq** - AI agent (localhost:8081)
- **PostgreSQL** - conversations, messages, sessions, QR codes
- **Telegram Bot** - Cross-channel sync with mobile app
- **duq-android** - Mobile client with wake word detection

## Security

See [SECURITY.md](SECURITY.md) for security policies and vulnerability reporting.

## License

MIT
