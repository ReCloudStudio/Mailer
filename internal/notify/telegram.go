package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/recloud/mailer/internal/config"
	"github.com/recloud/mailer/internal/mail"
)

type Telegram struct {
	token   string
	chatIDs []string
	apiBase string
	client  *http.Client
}

func NewTelegram(cfg config.Telegram) *Telegram {
	base := strings.TrimRight(cfg.APIBase, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Telegram{
		token:   cfg.BotToken,
		chatIDs: cfg.ChatIDs,
		apiBase: base,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *Telegram) Name() string { return "telegram" }

func (t *Telegram) Send(ctx context.Context, msg mail.Message) error {
	text := fmt.Sprintf("*%s*\n%s", escapeMarkdown(msg.Title()), escapeMarkdown(msg.Text()))

	var firstErr error
	for _, chatID := range t.chatIDs {
		if err := t.sendOne(ctx, chatID, text); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (t *Telegram) sendOne(ctx context.Context, chatID, text string) error {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "MarkdownV2",
		"disable_web_page_preview": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBase, t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram chat %s: status %d: %s", chatID, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"`", "\\`",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(s)
}
