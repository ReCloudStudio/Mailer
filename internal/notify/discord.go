package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/recloud/mailer/internal/config"
	"github.com/recloud/mailer/internal/mail"
)

// discordDest is a resolved delivery target for one account.
type discordDest struct {
	useBot     bool
	channelID  string // bot mode: channel OR thread id (a thread is a channel)
	webhookURL string // webhook mode base URL
	threadID   string // webhook mode: post into this thread (子区)
}

func (d discordDest) valid() bool {
	if d.useBot {
		return d.channelID != ""
	}
	return d.webhookURL != ""
}

// Discord pushes notifications via webhook or bot REST API, with optional
// per-account routing to a specific channel or thread.
type Discord struct {
	botToken string
	def      discordDest            // global default destination
	routes   map[string]discordDest // per-account overrides, keyed by account name
	client   *http.Client
}

// NewDiscord builds a Discord notifier. The accounts slice is used to resolve
// per-account channel/thread routing. If a bot token is configured, a Gateway
// connection is started in the background to show the bot as online.
func NewDiscord(cfg config.Discord, accounts []config.Account) (*Discord, error) {
	d := &Discord{
		botToken: cfg.BotToken,
		def:      resolveDest(cfg, nil),
		routes:   make(map[string]discordDest),
		client:   &http.Client{Timeout: 20 * time.Second},
	}

	for _, a := range accounts {
		if a.Discord == nil {
			continue
		}
		dest := resolveDest(cfg, a.Discord)
		if !dest.valid() {
			return nil, fmt.Errorf("discord: account %q resolves to an empty destination", a.Name)
		}
		d.routes[a.Name] = dest
	}

	if cfg.BotToken != "" {
		startGateway(context.Background(), cfg.BotToken)
	}

	return d, nil
}

// resolveDest computes the delivery target from the global config and an
// optional per-account override.
func resolveDest(cfg config.Discord, ad *config.AccountDiscord) discordDest {
	webhook := cfg.WebhookURL
	if ad != nil && ad.WebhookURL != "" {
		webhook = ad.WebhookURL
	}

	// Webhook transport takes precedence when a URL is available.
	if webhook != "" {
		dest := discordDest{useBot: false, webhookURL: webhook}
		if ad != nil && ad.Mode == config.DiscordModeThread {
			dest.threadID = ad.ThreadID
		}
		return dest
	}

	// Bot transport: a thread is itself a channel, so we just pick the right ID.
	channelID := cfg.ChannelID
	if ad != nil {
		switch {
		case ad.Mode == config.DiscordModeThread && ad.ThreadID != "":
			channelID = ad.ThreadID
		case ad.ChannelID != "":
			channelID = ad.ChannelID
		}
	}
	return discordDest{useBot: true, channelID: channelID}
}

// Name implements Notifier.
func (d *Discord) Name() string { return "discord" }

// Send delivers the message as a Discord embed to the account's destination.
func (d *Discord) Send(ctx context.Context, msg mail.Message) error {
	dest, ok := d.routes[msg.Account]
	if !ok {
		dest = d.def
	}
	if !dest.valid() {
		return fmt.Errorf("discord: no destination for account %q", msg.Account)
	}

	embed := map[string]any{
		"title":       truncateField(msg.Title(), 256),
		"description": truncateField(msg.Text(), 4000),
		"color":       0x2ecc71,
	}
	if !msg.Date.IsZero() {
		embed["timestamp"] = msg.Date.UTC().Format(time.RFC3339)
	}
	body, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return err
	}

	endpoint, auth := d.endpoint(dest)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("discord: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// endpoint returns the target URL and optional Authorization header.
func (d *Discord) endpoint(dest discordDest) (string, string) {
	if dest.useBot {
		u := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", dest.channelID)
		return u, "Bot " + d.botToken
	}
	u := dest.webhookURL
	if dest.threadID != "" {
		parsed, err := url.Parse(u)
		if err == nil {
			q := parsed.Query()
			q.Set("thread_id", dest.threadID)
			parsed.RawQuery = q.Encode()
			u = parsed.String()
		}
	}
	return u, ""
}

func truncateField(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
