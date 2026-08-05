# Mailer

*[English](README.md) | [简体中文](README.zh-CN.md)*

一个小巧的 Go 守护进程，定期检查一个或多个邮箱是否有新邮件，并将通知推送到
**Telegram** 和/或 **Discord** 机器人。

## 功能特性

- 按可配置的时间间隔并发轮询多个 IMAP 账户。
- 支持隐式 TLS（993 端口）或 STARTTLS（143 端口）。
- **IMAP 连接池**与 NOOP 保活 — 每个账户持有一条持久连接，无需每次轮询重新建立连接。
- 通过在 **SQLite** 数据库中记录每个账户已处理的最大 UID 来去重，
  邮件绝不会被重复通知——并且默认不会改动你的邮箱。
- **Message-ID 去重** — 数据库同时记录已投递的 `Message-ID`，可在邮件移动或
  UID 变更时捕获重复。
- 处理 `UIDVALIDITY` 变化（服务端重新编号）。
- 通知**指数退避重试**（可配置重试次数和延迟）。
- 可选：将已通知的邮件标记为 `\Seen`（已读）。
- **可选「标记已读」按钮** — 通知中附带交互按钮，点击即可在 IMAP 服务器上
  标记邮件为已读（Telegram 需要 bot_token；Discord 需要 bot_token，见下文）。
- 默认只通知程序启动*之后*到达的邮件（可配置）。
- 通知内容包含发件人、主题、日期以及纯文本正文预览（已做 MIME/字符集解码）。
- **可自定义消息模板** — 使用 Go `text/template` 自定义标题和正文格式，支持
  全局和每账户覆盖。
- 支持 Telegram（Bot API，**MarkdownV2** 模式）与 Discord（Webhook 或 Bot Token）投递。
- **每账户通知器路由** — 选择每个账户使用哪些通知渠道（Telegram、Discord 或两者）。
- **按账户分别路由 Discord**，可发送到指定的频道（channel）或子区（thread）。
- **通过文件路径或环境变量管理密钥** — `password_file`、`bot_token_file`、
  `webhook_url_file`；所有密钥字段支持 `${VAR}` / `$VAR` 展开。
- **健康检查（`/health`）+ Prometheus 指标（`/metrics`）** — 可配置 HTTP 端口。
- 无 CGO 构建 → 生成体积小巧的多架构（amd64/arm64）Docker 镜像。
- 收到 SIGINT/SIGTERM 时优雅退出。

## 多账户

`accounts:` 列表支持任意数量的邮箱——直接添加更多条目即可。它们会被**并发**
轮询，并各自维护独立的去重状态（以 `name` 作为键，因此请为每个账户设置唯一的
`name`）：

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
    password: "app-password"   # Gmail 需要使用「应用专用密码」
    tls: true
```

## 构建

```bash
go build -o mailer .
```

## 配置

复制示例文件并修改：

```bash
cp config.example.yaml config.yaml
```

主要字段在 `config.example.yaml` 中都有内联注释说明。

### Telegram 配置
1. 通过 [@BotFather](https://t.me/BotFather) 创建机器人并复制 token。
2. 给你的机器人发一条消息，然后访问
   `https://api.telegram.org/bot<TOKEN>/getUpdates` 查找你的数字 `chat_id`。
3. 设置 `telegram.enabled: true`、`bot_token` 和 `chat_ids`。

### Discord 配置
- **Webhook（最简单）：** 频道设置 → 整合 → Webhook → 新建 Webhook →
  将 URL 复制到 `discord.webhook_url`。
- **Bot：** 创建应用/机器人，以 `Send Messages` 权限邀请它，然后设置
  `discord.bot_token` 和 `discord.channel_id`。

#### 按账户路由：频道（channel）与子区（thread）

每个账户都可以覆盖全局的 Discord 目标，让不同邮箱把通知发送到不同的频道或子区。
在账户下添加一个 `discord:` 块即可：

```yaml
discord:                     # 全局默认值
  enabled: true
  bot_token: "your-bot-token"
  channel_id: "111111111111" # 未单独设置路由的账户使用此默认频道

accounts:
  - name: primary
    # ...IMAP 相关字段...
    discord:
      mode: channel          # 频道
      channel_id: "222222222222"

  - name: work
    # ...IMAP 相关字段...
    discord:
      mode: thread           # 子区
      thread_id: "333333333333"
```

- `mode: channel` 发送到 `channel_id`。
- `mode: thread` 发送到 `thread_id`（对 Bot 而言子区本身就是一个频道；
   对 Webhook 则通过 `?thread_id=` 参数发送）。
- 使用 **Webhook** 时，也可以为账户单独提供 `webhook_url`；由于一个 Webhook
   绑定到它自己的频道，若要发往不同频道，请使用不同的 Webhook（或改用 Bot 方式）。

### 每账户通知器选择

默认所有启用的通知器都会收到每条通知。要限制某个账户使用哪些通知器，请在
账户下设置 `notifiers`：

```yaml
telegram:
  enabled: true
discord:
  enabled: true

accounts:
  - name: quiet
    # ...IMAP 相关字段...
    notifiers: [discord]        # 仅 Discord，不含 Telegram
```

## 已读按钮（read_button）

可选开启交互式「标记已读」按钮，直接点击通知即可将对应邮件在 IMAP 服务器上
标记为 `\Seen`，无需登录邮箱客户端。

```yaml
read_button: true
```

要求：

- **Telegram：** 需要 `telegram.bot_token`。程序通过 `getUpdates` 长轮询接收
  按钮点击，并调用 `answerCallbackQuery` 反馈结果。
- **Discord：** 需要 `discord.bot_token`（按钮交互必须由 Bot 的 Gateway 处理）。
  纯 **Webhook** 模式无法接收按钮点击，按钮会处于不可用状态——程序会在启动时
  打印警告。此外，用 Bot 的 REST API（`channel_id`）发送的消息按钮必然可用；
  若用 Webhook 发送，则该 Webhook 必须由 Bot 应用创建，点击事件才会派发给 Bot。

> 注意：按钮触发的是**即时**标记操作，仅在点击时通过连接池的 IMAP 连接执行
> `STORE +FLAGS \Seen`，不会影响 `mark_seen`（通知后自动已读）等现有行为。

## 连接池

每个账户持有一条持久 IMAP 连接。每次轮询结束后连接归还到池中，并通过 NOOP
命令在可配置的 `noop_interval`（默认 60 秒）间隔下保活。如果 NOOP 超时（10 秒），
连接会被关闭，下次轮询时创建新连接。

## 通知重试

失败的通知会以指数退避策略重试：

```yaml
retry_attempts: 3    # 每条消息最大重试次数（默认：2）
retry_delay: 5s      # 基础延迟，每次尝试加倍（默认：5 秒）
```

所有重试用尽后，该消息被跳过并记录日志。

## 邮件去重（Message-ID）

除基于 UID 的追踪外，`seen_messages` 表记录了每个成功投递的 `Message-ID`。
这能捕获邮件在文件夹间移动（新 UID）或服务端重新编号（UID validity 变更）时
产生的重复。无需额外配置。

## 自定义消息模板

使用 Go [`text/template`](https://pkg.go.dev/text/template) 自定义通知格式：

```yaml
message_template:
  title: "[{{.Subject}}]"
  text: |
    **发件人：** {{.From}}
    **日期：** {{.Date}}
    {{.Preview}}...
    {{"\n"}}`{{.MessageID}}`
```

全局模板适用于所有账户；账户级的 `message_template` 可覆盖全局设置。

可用字段：`{{.From}}`、`{{.Subject}}`、`{{.Date}}`、`{{.Preview}}`、
`{{.MessageID}}`、`{{.Text}}`（完整正文）、`{{.Account}}`（配置中的账户名称）。

## 健康检查与 Prometheus 指标

内置 HTTP 服务器（默认端口 **9100**）提供两个端点：

| 端点          | 说明                                        |
|---------------|---------------------------------------------|
| `GET /health` | 返回 `{"status":"ok"}` — 适用于容器健康检查。 |
| `GET /metrics` | Prometheus 文本格式 — 轮询次数、通知次数、错误数。 |

通过 `health_port` 配置端口（设为 `0` 可禁用服务器）。

## 密钥管理

所有凭证字段支持两种来源（按顺序处理）：

1. **文件路径** — 例如 `password_file: /run/secrets/imap_password`（从文件读取，
   去除首尾空白）。
2. **环境变量展开** — 例如 `password: ${IMAP_PASSWORD}`，通过 `os.ExpandEnv` 实现。

在 Docker 中可以使用挂载的文件密钥：

```yaml
password_file: /run/secrets/imap_pass
```

或者环境变量（例如在 docker-compose 中）：

```yaml
password: ${IMAP_PASSWORD}
```

`bot_token` / `bot_token_file`（Telegram 和 Discord）以及 `webhook_url` /
`webhook_url_file`（Discord）同样适用。

## 运行

```bash
./mailer -config config.yaml
```

## 使用 Docker 运行

多阶段 `Dockerfile` 会生成一个极小的静态镜像（Alpine + CA 证书），以非 root
用户运行。构建过程无 CGO，因此同时发布 `linux/amd64` 和 `linux/arm64` 镜像。

### 拉取预构建镜像（GHCR）

每次推送到默认分支以及每个 `v*` 标签，都会通过 `.github/workflows/docker.yml`
中的工作流构建并发布镜像到 GitHub Container Registry：

```bash
docker pull ghcr.io/ReCloudStudio/mailer:latest
```

### 直接构建并运行

```bash
docker build -t mailer .

docker run -d --name mailer --restart unless-stopped \
  -e TZ=Asia/Shanghai \
  -v "$PWD/config.yaml:/app/config.yaml:ro" \
  -v "$PWD/data:/app/data" \
  mailer
```

### docker compose（推荐）

```bash
mkdir -p data                 # 可写的状态目录
cp config.example.yaml config.yaml
# 在 config.yaml 中设置：  state_file: /app/data/state.db
docker compose up -d
docker compose logs -f
```

> 容器以 UID `10001` 运行。请确保挂载的 `./data` 目录对其可写，例如
> `sudo chown -R 10001:10001 data`，并设置 `state_file: /app/data/state.db`，
> 使去重状态能在重启后保留。

## 作为 systemd 服务运行

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

## 去重原理

两层机制防止重复通知：

1. **UID 追踪** — 每个账户的最新 IMAP UID 存储在 `account_state` 表中。每次
   轮询搜索 `UID > last_uid`，因此只会拉取真正的新邮件。首次运行时会把当前
   最新的 UID 记录为基准（除非设置了 `notify_existing: true`），这样你就不会
   被整个收件箱的历史邮件刷屏。

2. **Message-ID 去重** — `seen_messages` 表记录每个成功投递的 `Message-ID`。
   当邮件在文件夹间移动或服务端重新编号时，其 `Message-ID` 仍能被识别并跳过。

## 项目结构

```
main.go                       入口，命令行参数解析、信号处理，
                              健康检查与指标 HTTP 服务器
internal/config               YAML 配置加载、默认值、校验
internal/mail                 IMAP 拉取、MIME 预览提取、标记已读
internal/mail/pool.go         IMAP 连接池与 NOOP 保活
internal/notify               Telegram (+MarkdownV2) + Discord 通知器
internal/state                基于 SQLite 的 UID 进度与 Message-ID 去重
internal/app                  将拉取 + 通知 + 状态串联起来的调度器
internal/app/metrics.go       Prometheus 指标计数器
.github/workflows             构建并推送 Docker 镜像到 GHCR 的 CI
```
