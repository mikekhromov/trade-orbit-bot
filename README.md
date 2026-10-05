# Trade Orbit Telegram bot

This repository owns the Telegram bot image, Compose project, and every command
that starts or deploys the bot. It runs independently from the core API stack.

## Configure the bot server

Clone this repo to `/opt/trade-orbit-bot`. On supported Ubuntu and Debian
systems, `make start` installs Docker Engine, Compose and Buildx if they are
missing. The installer requires `sudo` access. From that checkout, create the
environment file:

```bash
git clone git@github.com:mikekhromov/trade-orbit-bot.git /opt/trade-orbit-bot
cd /opt/trade-orbit-bot
cp .env.example .env
chmod 600 .env
nano .env
```

Set these values:

- `TOKEN_TG_BOT` — token from BotFather.
- `CORE_API_URL` — core API address reachable from the bot server, such as
  `http://CORE_SERVER_IP:8080` for the current pilot.
- `INTERNAL_SERVICE_TOKEN` — the same secret configured for the core API.
- `TELEGRAM_API_BASE_URL` — keep `https://api.telegram.org`.
- The health endpoint listens on the internal container address `:8081`; it is
  not published on the host.

Because the bot and API are on different servers, `http://core-api:8080` will
not resolve. For the current pilot, set `CORE_API_URL` to the core server's
public address. In the API repository's `.env`, set
`CORE_API_BIND_ADDRESS=0.0.0.0`, redeploy the API, and restrict inbound TCP
port `8080` in the provider firewall to the bot server's fixed public IP.
Use the same strong, randomly generated `INTERNAL_SERVICE_TOKEN` in both
repositories; the bot sends it as a Bearer token to `/internal/v1/*` routes.

This pilot sends that token over plain HTTP. Keep the source-IP firewall rule;
HTTPS transport is deferred and recorded in OpenSpec in the core repository.

## Run on the bot server

From the bot checkout on the Netherlands VDS, after configuring `.env`, run:

```bash
make start
```

This checks Docker and its Compose plugin, installs them if needed, builds and
starts only `telegram-bot`, then waits for its healthcheck.
Other commands from the repository root:

```bash
make status
make logs
make restart
make stop
make test
make lint
```

`make logs` follows the log stream; press Ctrl-C to stop following logs without
stopping the bot. The health endpoint is not published on a host port.

## Deploy from a development machine

The deploy command transfers the bot image and Compose file over SSH. The target
must already have Docker Compose and `/opt/trade-orbit-bot/.env` configured:

```bash
cd ~/Documents/code/trading/bot
DEPLOY_TARGET=root@NETHERLANDS_SERVER_IP make deploy
```

The default image platform is `linux/amd64`. For an ARM server, use:

```bash
DEPLOY_TARGET=root@NETHERLANDS_SERVER_IP DEPLOY_PLATFORM=linux/arm64 make deploy
```

The deploy script prints the release tag after a successful healthcheck. To
roll back to a previously deployed tag:

```bash
DEPLOY_TARGET=root@NETHERLANDS_SERVER_IP make rollback TAG=RELEASE_TAG
```

Do not run a second bot instance with the same token; both instances would
compete for Telegram `getUpdates`.
