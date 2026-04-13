# Google Apps Script - Calendar Reminder

Скрипт для отправки напоминаний о событиях Google Calendar в Duq.

## Как работает

```
Google Calendar
      ↓ (каждые 5 мин)
Google Apps Script
      ↓ (за 15 мин до события)
POST /api/calendar
      ↓
Duq Gateway
      ↓
Telegram
```

## Установка

### 1. Создать проект в Google Apps Script

1. Перейти на [script.google.com](https://script.google.com)
2. Нажать "Новый проект"
3. Скопировать содержимое `calendar-reminder.gs`
4. Сохранить (Ctrl+S)

### 2. Настроить Script Properties

1. В редакторе: **File → Project Properties → Script Properties**
2. Добавить свойства:

| Property | Value |
|----------|-------|
| `WEBHOOK_URL` | `https://on-za-menya.online/api/calendar` |
| `WEBHOOK_TOKEN` | Токен из `/etc/duq-gateway/config.json` |
| `REMINDER_MINUTES` | `15` (опционально) |
| `CALENDAR_ID` | `primary` (опционально) |

### 3. Авторизовать скрипт

1. В редакторе: Run → Run function → `testReminder`
2. Разрешить доступ к Calendar и External URLs
3. Проверить что тестовое напоминание пришло в Telegram

### 4. Установить триггер

1. Run → Run function → `setupTrigger`
2. Скрипт будет запускаться каждые 5 минут

## Функции

| Функция | Описание |
|---------|----------|
| `checkUpcomingEvents()` | Главная функция, запускается по триггеру |
| `setupTrigger()` | Создать триггер (запустить 1 раз) |
| `removeTriggers()` | Удалить все триггеры |
| `testReminder()` | Отправить тестовое напоминание |

## Troubleshooting

### Напоминания не приходят

1. Проверить логи: View → Logs
2. Проверить Script Properties
3. Запустить `testReminder()` для теста

### Ошибка авторизации

1. Run → Run function → любая функция
2. Разрешить доступ

### Двойные напоминания

Скрипт хранит обработанные события в кеше на 24 часа.
Если кеш очистился - может прийти повторное напоминание.

## Безопасность

- Токен хранится в Script Properties (не в коде)
- Скрипт работает только с вашим календарём
- HTTPS соединение с webhook
