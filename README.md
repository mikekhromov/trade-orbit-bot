# Trade Orbit Telegram bot

Standalone Telegram bot service for Trade Orbit. It polls Telegram updates,
renders charts, and calls the core API over HTTP. It does not access PostgreSQL.

## Configuration

Set these environment variables when running the service:

- `TOKEN_TG_BOT` — Telegram bot token (required)
- `INTERNAL_SERVICE_TOKEN` — shared secret used for bot-to-API requests
- `CORE_API_URL` — core API base URL (default `http://core-api:8080`)
- `TELEGRAM_API_BASE_URL` — Telegram API URL (default `https://api.telegram.org`)
- `HTTP_ADDR` — health endpoint address (default `:8081`)

Keep `INTERNAL_SERVICE_TOKEN` equal to the value configured on the core API.
Store both secrets in the deployment environment, never in this repository.

## Build and test

```bash
go test ./...
go vet ./...
go build ./cmd/bot
docker build -t trade-orbit/telegram-bot:current .
```

## Выкладка на сервер

Бот использует общий Docker Compose и сеть с `core-api`. Сначала разверни
основной стек из репозитория `trading/apps`. На сервере должны быть файлы
`/opt/trade-orbit/compose.yaml` и `/opt/trade-orbit/.env`.

В серверном `.env` задай `TOKEN_TG_BOT` и `INTERNAL_SERVICE_TOKEN`. Значение
`INTERNAL_SERVICE_TOKEN` должно совпадать с настроенным для core API. Не копируй
`.env` в репозиторий и не передавай его в командной строке.

На машине, с которой выкладываешь проект, проверь SSH-доступ к серверу и запусти:

```bash
cd ~/Documents/code/trading/bot
go test ./...
DEPLOY_TARGET=root@SERVER_IP ./shell-tools/deploy.sh
```

Скрипт собирает образ для `linux/amd64`, передаёт его серверу через SSH и
пересоздаёт только `telegram-bot`, ожидая успешного healthcheck. Для ARM-сервера
задай `DEPLOY_PLATFORM=linux/arm64`. Путь по умолчанию — `/opt/trade-orbit`;
его можно изменить переменной `DEPLOY_PATH`.

Проверить состояние и последние логи можно так:

```bash
ssh root@SERVER_IP 'cd /opt/trade-orbit && docker compose --env-file .env -f compose.yaml ps telegram-bot'
ssh root@SERVER_IP 'cd /opt/trade-orbit && docker compose --env-file .env -f compose.yaml logs --tail=100 telegram-bot'
```

Скрипт печатает использованный release tag. Чтобы откатить бота к предыдущему
образу, выполни на сервере:

```bash
cd /opt/trade-orbit
./rollback.sh telegram-bot RELEASE_TAG
```

Не запускай второй экземпляр с тем же `TOKEN_TG_BOT`: оба будут конкурировать
за Telegram `getUpdates`.
