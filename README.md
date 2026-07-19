# Mailer

*[English](README.md) | [简体中文](README.zh-CN.md)*

A small Go daemon that periodically checks one or more mailboxes for new email
and pushes a notification to **Telegram** and/or **Discord** bots.

## Features

- Polls multiple IMAP accounts concurrently on a configurable interval.
- Implicit TLS (port 993) or STARTTLS (port 143).
- **IMAP connection pool** with NOOP keepalive — one persistent connection per
  account instead of reconnecting every poll cycle.
- De-duplicates by tracking the last processed UID per account in a **SQLite**
  database, so mail is never notified twice — without altering your mailbox by
  default.
- **Message-ID de-duplication** — the database also remembers which
  `Message-ID` values have been notified, catching duplicates across mailbox
  moves or UID changes.
- Handles `UIDVALIDITY` changes (server-side renumbering).
- Notification **retry with exponential backoff** (configurable attempts & delay).
- Optional: mark notified mail as `\Seen`.
- By default only notifies mail that arrives *after* startup (configurable).
- Notifications include sender, subject, date, and a plain-text body preview
  (MIME/charset decoded).
- **Customizable message templates** — use Go `text/template` to format title
  and body globally or per-account.
- Telegram (Bot API, **MarkdownV2** mode) and Discord (webhook or bot token)
  delivery.
- **Per-account notifier routing** — choose which notifiers (Telegram, Discord,
  or both) apply to each account.
- **Per-account Discord routing** to a specific channel (频道) or thread (子区).
- **Secrets via file path or environment variables** — `password_file`,
  `bot_token_file`, `webhook_url_file`; all secret fields support
  `${VAR}` / `$VAR` expansion.
- **Health check (`/health`) + Prometheus metrics (`/metrics`)** on a
  configurable HTTP port.
- CGO-free build → tiny multi-arch (amd64/arm64) Docker image.
- Graceful shutdown on SIGINT/SIGTERM.

## Multiple accounts

The `accounts:` list supports any number of mailboxes — just add more entries.
They are polled **concurrently** and each keeps its own de-duplication state
(keyed by `name`, so give every account a unique `name`):

```yaml
accounts:
  - name: primary
    host: imap.example.com
    username: me@example.com
    password: "app-password"
    tls: true

  - name: work
    host: outlook.office365.com
    username: me@work.com
    password: "app-password"
    tls: true

  - name: gmail
    host: imap.gmail.com
    username: me@gmail.com
    password: "app-password"   # Gmail needs an App Password
    tls: true
```

## Build

```bash
go build -o mailer .
```

## Configure

Copy the example and edit it:

```bash
cp config.example.yaml config.yaml
```

Key fields are documented inline in `config.example.yaml`.

### Telegram setup
1. Create a bot via [@BotFather](https://t.me/BotFather) and copy the token.
2. Send a message to your bot, then read
   `https://api.telegram.org/bot<TOKEN>/getUpdates` to find your numeric
   `chat_id`.
3. Set `telegram.enabled: true`, `bot_token`, and `chat_ids`.

### Discord setup
- **Webhook (simplest):** Channel settings → Integrations → Webhooks → New
  Webhook → copy URL into `discord.webhook_url`.
- **Bot:** create an application/bot, invite it with `Send Messages`, then set
  `discord.bot_token` and `discord.channel_id`.

#### Per-account routing: channel (频道) vs thread (子区)

Each account can override the global Discord destination so different mailboxes
post to different channels or threads. Add a `discord:` block under the account:

```yaml
discord:                     # global defaults
  enabled: true
  bot_token: "your-bot-token"
  channel_id: "111111111111" # fallback for accounts without a route

accounts:
  - name: primary
    # ...imap fields...
    discord:
      mode: channel          # 频道
      channel_id: "222222222222"

  - name: work
    # ...imap fields...
    discord:
      mode: thread           # 子区
      thread_id: "333333333333"
```

- `mode: channel` posts to `channel_id`.
- `mode: thread` posts to `thread_id` (a thread is itself a channel for bots;
  for webhooks the message is sent with `?thread_id=`).
- With a **webhook**, a per-account `webhook_url` can also be supplied; a
  webhook is bound to its own channel, so use a different webhook (or the bot
  transport) to target a different channel.

### Per-account notifier selection

By default all enabled notifiers receive every notification. To restrict which
notifiers fire for a particular account, list them under `notifiers`:

```yaml
telegram:
  enabled: true
discord:
  enabled: true

accounts:
  - name: quiet
    # ...imap fields...
    notifiers: [discord]        # only Discord, no Telegram
```

## Connection pool

Each account holds one persistent IMAP connection. After every poll the
connection is returned to the pool and kept alive with NOOP commands on a
configurable `noop_interval` (default: 60 s). If a NOOP times out (10 s), the
connection is closed and a new one is created on the next poll.

## Notification retry

Failed notifications are retried with exponential backoff:

```yaml
retry_attempts: 3    # max retries per message (default: 2)
retry_delay: 5s      # base delay, doubled each attempt (default: 5 s)
```

If all retries are exhausted the message is skipped and logged.

## Message de-duplication (Message-ID)

In addition to UID-based tracking, the `seen_messages` table records every
successfully delivered `Message-ID`. This catches duplicates that occur when a
message moves between folders (new UID) or the server renumbers (UID validity
change). No extra configuration is needed.

## Customizable message templates

Use Go [`text/template`](https://pkg.go.dev/text/template) to customise
notification format:

```yaml
message_template:
  title: "[{{.Subject}}]"
  text: |
    **From:** {{.From}}
    **Date:** {{.Date}}
    {{.Preview}}...
    {{"\n"}}`{{.MessageID}}`
```

The global template applies to every account; an account-level
`message_template` overrides it for that account alone.

Available fields: `{{.From}}`, `{{.Subject}}`, `{{.Date}}`, `{{.Preview}}`,
`{{.MessageID}}`, `{{.Text}}` (full body), `{{.Account}}` (config account name).

## Health check & Prometheus metrics

A built-in HTTP server (default port **9100**) exposes two endpoints:

| Endpoint    | Description                           |
|-------------|---------------------------------------|
| `GET /health` | Returns `{"status":"ok"}` — ideal for container health checks. |
| `GET /metrics` | Prometheus text format — poll counts, notification counts, errors. |

Configure the port with `health_port` (set to `0` to disable the server).

## Secrets management

All credential fields support two sources (evaluated in order):

1. **File path** — e.g. `password_file: /run/secrets/imap_password` (read from
   file, whitespace trimmed).
2. **Environment variable expansion** — e.g. `password: ${IMAP_PASSWORD}` via
   `os.ExpandEnv`.

In Docker you can use secrets mounted as files:

```yaml
password_file: /run/secrets/imap_pass
```

Or environment variables (e.g. in docker-compose):

```yaml
password: ${IMAP_PASSWORD}
```

The same applies to `bot_token` / `bot_token_file` (Telegram & Discord) and
`webhook_url` / `webhook_url_file` (Discord).

## Run

```bash
./mailer -config config.yaml
```

## Run with Docker

A multi-stage `Dockerfile` produces a tiny static image (Alpine + CA certs),
running as a non-root user. The build is CGO-free, so images are published for
both `linux/amd64` and `linux/arm64`.

### Pull the prebuilt image (GHCR)

Every push to the default branch and every `v*` tag builds and publishes an
image to the GitHub Container Registry via the workflow in
`.github/workflows/docker.yml`:

```bash
docker pull ghcr.io/ReCloudStudio/mailer:latest
```

### Build & run directly

```bash
docker build -t mailer .

docker run -d --name mailer --restart unless-stopped \
  -e TZ=Asia/Shanghai \
  -v "$PWD/config.yaml:/app/config.yaml:ro" \
  -v "$PWD/data:/app/data" \
  mailer
```

### docker compose (recommended)

```bash
mkdir -p data                 # writable state directory
cp config.example.yaml config.yaml
# In config.yaml set:  state_file: /app/data/state.db
docker compose up -d
docker compose logs -f
```

> The container runs as UID `10001`. Make sure the mounted `./data` directory
> is writable by it, e.g. `sudo chown -R 10001:10001 data`, and set
> `state_file: /app/data/state.db` so de-duplication state survives restarts.

## Run as a systemd service

```ini
# /etc/systemd/system/mailer.service
[Unit]
Description=Mailer IMAP -> Telegram/Discord notifier
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/mailer/mailer -config /opt/mailer/config.yaml
WorkingDirectory=/opt/mailer
Restart=always
RestartSec=10
User=mailer

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now mailer
```

## How de-duplication works

Two layers prevent duplicate notifications:

1. **UID tracking** — For each account the highest seen IMAP UID is stored in
   the `account_state` table. Each poll searches for `UID > last_uid`, so only
   genuinely new mail is fetched. On first run the current newest UID is
   recorded as the baseline (unless `notify_existing: true`), so you are not
   flooded with your entire inbox history.

2. **Message-ID dedup** — The `seen_messages` table records every successfully
   delivered `Message-ID`. When a message is moved between folders or the
   server renumbers (UID validity change), its `Message-ID` is still recognised
   and skipped.

## Project layout

```
main.go                       entrypoint, flag parsing, signal handling,
                              health & metrics HTTP server
internal/config               YAML config loading, defaults, validation
internal/mail                 IMAP fetch, MIME preview extraction, mark-seen
internal/mail/pool.go         IMAP connection pool with NOOP keepalive
internal/notify               Telegram (+MarkdownV2) + Discord notifiers
internal/state                SQLite-backed UID progress + Message-ID dedup
internal/app                  scheduler tying fetch + notify + state together
internal/app/metrics.go       Prometheus metric counters
.github/workflows             CI to build & push the Docker image to GHCR
```
