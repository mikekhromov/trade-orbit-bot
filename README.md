# Telegram-бот Trade Orbit

Этот репозиторий содержит образ Telegram-бота, Compose-проект и команды
сборки, запуска и развёртывания. Бот работает отдельно от стека Core API.

## Настройка сервера бота

Склонируйте репозиторий в `/opt/trade-orbit-bot`. На поддерживаемых версиях
Ubuntu и Debian команда `make start` при необходимости установит Docker Engine,
Compose и Buildx. Установщику требуется доступ через `sudo`. Создайте файл
настроек:

```bash
git clone git@github.com:mikekhromov/trade-orbit-bot.git /opt/trade-orbit-bot
cd /opt/trade-orbit-bot
cp .env.example .env
chmod 600 .env
nano .env
```

Укажите значения:

- `TOKEN_TG_BOT` — токен бота от BotFather.
- `CORE_API_URL` — адрес Core API, доступный с сервера бота, например
  `http://CORE_API_IP:8080`.
- `INTERNAL_SERVICE_TOKEN` — тот же секрет, что указан в `.env` Core API.
- `TELEGRAM_API_BASE_URL` — оставьте `https://api.telegram.org`.
- Проверка состояния слушает внутренний адрес контейнера `:8081`; порт не
  публикуется на сервере.

## Связь с Core API

Когда бот и API находятся на разных серверах, имя `core-api` не разрешается
между ними. Укажите в `CORE_API_URL` публичный IP или доменное имя сервера API.
В `.env` API задайте `CORE_API_BIND_ADDRESS=0.0.0.0`, затем примените настройки
на сервере API командой `sudo bash shell-tools/start-production.sh` из корня
репозитория API.

В сетевом экране хостинга API разрешите TCP-порт `8080` только с фиксированного
публичного IP сервера бота. Используйте один длинный случайный
`INTERNAL_SERVICE_TOKEN` в обоих `.env`: бот передаёт его как Bearer-токен для
маршрутов `/internal/v1/*`.

Проверьте доступность API с сервера бота:

```bash
curl -i --max-time 5 http://АДРЕС_API:8080/healthz
```

Ожидается HTTP `200`. Если соединение отклонено, проверьте bind-адрес API,
сетевой экран и правильность `CORE_API_URL`. Если API отвечает `401` на команду
бота, сравните `INTERNAL_SERVICE_TOKEN` в обоих `.env`.

В текущем пилоте токен передаётся по незашифрованному HTTP. Сохраняйте правило
сетевого экрана, разрешающее доступ только с IP сервера бота. Настройка HTTPS
между серверами отложена и описана в OpenSpec репозитория API.

## Запуск на сервере бота

После настройки `.env` выполните из каталога бота:

```bash
make start
```

Команда проверит Docker и Compose, установит недостающие компоненты, соберёт
образ и запустит только `telegram-bot`, затем дождётся успешной проверки
состояния. Без `make` можно выполнить `bash shell-tools/start.sh`.

Остальные команды из корня репозитория:

```bash
make status
make logs
make restart
make stop
make test
make lint
```

Команда `make logs` показывает поток журналов; нажмите Ctrl-C, чтобы прекратить
просмотр, не останавливая бота. Проверочный адрес контейнера не опубликован на
сервере.

Если бот отвечает «Не удалось подключить Trade Orbit», проверьте журнал:

```bash
docker compose --env-file .env -f compose.yaml logs --since=10m telegram-bot
```

Сообщение `connect: connection refused` указывает на недоступный порт API;
`401 Unauthorized` — на несовпадающие внутренние токены. Повторите `/start`
после исправления конфигурации.

## Развёртывание с компьютера разработчика

Команда развёртывания передаёт образ и Compose-файл по SSH. На сервере должны
быть Docker Compose и настроенный `/opt/trade-orbit-bot/.env`:

```bash
cd ~/Documents/code/trading/bot
DEPLOY_TARGET=root@IP_СЕРВЕРА_БОТА make deploy
```

По умолчанию собирается образ для `linux/amd64`. Для ARM-сервера выполните:

```bash
DEPLOY_TARGET=root@IP_СЕРВЕРА_БОТА DEPLOY_PLATFORM=linux/arm64 make deploy
```

После успешной проверки состояния скрипт выводит тег выпуска. Чтобы вернуться
к предыдущему выпуску:

```bash
DEPLOY_TARGET=root@IP_СЕРВЕРА_БОТА make rollback TAG=ТЕГ_ВЫПУСКА
```

Не запускайте два экземпляра бота с одним токеном: они будут конкурировать за
обновления Telegram через `getUpdates`.
