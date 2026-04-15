# ⛔⛔⛔ ЗАПРЕТ ЛОКАЛЬНОГО ТЕСТИРОВАНИЯ ⛔⛔⛔

## КАТЕГОРИЧЕСКИ ЗАПРЕЩЕНО:
- Запускать docker контейнеры локально
- Создавать .venv локально
- Запускать pytest/go test локально
- Создавать локальные БД
- Запускать сервисы локально
- ЛЮБОЕ локальное тестирование

## НИКОГДА НЕ ИСПОЛЬЗУЙ:
- `sleep` в командах
- `git clone` на VPS
- Плейсхолдеры или заглушки

## ПЕРЕД СЛОВОМ "ГОТОВО":
1. `grep -r "удаляемый_паттерн"` по ВСЕМУ проекту
2. Компиляция ДОЛЖНА пройти
3. Сервис ДОЛЖЕН запуститься на VPS

## ВСЕ ТЕСТИРОВАНИЕ ТОЛЬКО НА VPS

### Сервер:
```
IP: 90.156.230.49
SSH: ssh root@90.156.230.49
```

### Сервисы:
```
duq.service      -> /opt/duq/current       -> :8081
duq-gateway      -> /opt/duq-gateway       -> :8082
PostgreSQL       -> duq database
Redis            -> duq:* keys
```

## ДЕПЛОЙ GATEWAY (Go):
```bash
# 1. Локальный билд (ОБЯЗАТЕЛЬНО с флагами оптимизации!)
cd /home/danny/Documents/projects/duq-gateway
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o gateway-bin .

# 2. Остановить сервис
ssh root@90.156.230.49 "systemctl stop duq-gateway; rm -f /opt/duq-gateway/duq-gateway"

# 3. Скопировать бинарник
scp gateway-bin root@90.156.230.49:/opt/duq-gateway/duq-gateway

# 4. Запустить
ssh root@90.156.230.49 "chmod +x /opt/duq-gateway/duq-gateway && systemctl start duq-gateway && systemctl status duq-gateway --no-pager"
```

## ДЕПЛОЙ DUQ (Python):
```bash
ssh root@90.156.230.49 "cd /opt/duq/current && git pull && source .venv/bin/activate && pip install -e . && systemctl restart duq"
```

## НАРУШЕНИЕ = НЕМЕДЛЕННОЕ ПРЕКРАЩЕНИЕ РАБОТЫ
