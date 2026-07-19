package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	path := writeTemp(t, `
accounts:
  - host: imap.example.com
    username: me@example.com
    password: secret
    tls: true
telegram:
  enabled: true
  bot_token: "t"
  chat_ids: ["1"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollInterval != 60*time.Second {
		t.Errorf("PollInterval = %v, want 60s", cfg.PollInterval)
	}
	if cfg.StateFile != "state.db" {
		t.Errorf("StateFile = %q, want state.db", cfg.StateFile)
	}
	a := cfg.Accounts[0]
	if a.Port != 993 {
		t.Errorf("Port = %d, want 993 for TLS", a.Port)
	}
	if a.Mailbox != "INBOX" {
		t.Errorf("Mailbox = %q, want INBOX", a.Mailbox)
	}
	if a.Name != "me@example.com" {
		t.Errorf("Name = %q, want username fallback", a.Name)
	}
}

func TestPortDefaultSTARTTLS(t *testing.T) {
	path := writeTemp(t, `
accounts:
  - host: imap.example.com
    username: u
    password: p
    tls: false
discord:
  enabled: true
  webhook_url: "https://x"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Accounts[0].Port != 143 {
		t.Errorf("Port = %d, want 143 for non-TLS", cfg.Accounts[0].Port)
	}
}

func TestValidateNoNotifier(t *testing.T) {
	path := writeTemp(t, `
accounts:
  - host: h
    username: u
    password: p
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error when no notifier enabled")
	}
}

func TestDiscordThreadRequiresThreadID(t *testing.T) {
	path := writeTemp(t, `
accounts:
  - name: a
    host: h
    username: u
    password: p
    discord:
      mode: thread
discord:
  enabled: true
  bot_token: "b"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error: thread mode without thread_id")
	}
}

func TestDiscordPerAccountRoutingDefaults(t *testing.T) {
	path := writeTemp(t, `
accounts:
  - name: a
    host: h
    username: u
    password: p
    discord:
      channel_id: "123"
  - name: b
    host: h
    username: u
    password: p
    discord:
      mode: thread
      thread_id: "999"
discord:
  enabled: true
  bot_token: "b"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Accounts[0].Discord.Mode; got != DiscordModeChannel {
		t.Errorf("account a mode = %q, want default %q", got, DiscordModeChannel)
	}
	if got := cfg.Accounts[1].Discord.Mode; got != DiscordModeThread {
		t.Errorf("account b mode = %q, want %q", got, DiscordModeThread)
	}
}

func TestValidateNoAccounts(t *testing.T) {
	path := writeTemp(t, `
telegram:
  enabled: true
  bot_token: t
  chat_ids: ["1"]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error when no accounts configured")
	}
}
