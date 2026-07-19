package state

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type AccountState struct {
	LastUID     uint32
	UIDValidity uint32
}

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS account_state (
    account      TEXT    PRIMARY KEY,
    last_uid     INTEGER NOT NULL DEFAULT 0,
    uid_validity INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE TABLE IF NOT EXISTS seen_messages (
    message_id TEXT    NOT NULL,
    account    TEXT    NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    PRIMARY KEY (message_id, account)
);
`

func Load(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	db.SetMaxOpenConns(1)

	// Ensure both tables exist. WAL mode may persist schema across runs.
	for _, stmt := range []string{schema} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("init schema: %w", err)
		}
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Get(account string) (AccountState, bool) {
	var st AccountState
	row := s.db.QueryRow(
		`SELECT last_uid, uid_validity FROM account_state WHERE account = ?`,
		account,
	)
	if err := row.Scan(&st.LastUID, &st.UIDValidity); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[state] get %q: %v", account, err)
		}
		return AccountState{}, false
	}
	return st, true
}

func (s *Store) Set(account string, st AccountState) error {
	_, err := s.db.Exec(`
INSERT INTO account_state (account, last_uid, uid_validity, updated_at)
VALUES (?, ?, ?, strftime('%s','now'))
ON CONFLICT(account) DO UPDATE SET
    last_uid     = excluded.last_uid,
    uid_validity = excluded.uid_validity,
    updated_at   = excluded.updated_at`,
		account, st.LastUID, st.UIDValidity,
	)
	if err != nil {
		return fmt.Errorf("save state for %s: %w", account, err)
	}
	return nil
}

func (s *Store) IsDuplicate(account, messageID string) (bool, error) {
	if messageID == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM seen_messages WHERE message_id = ? AND account = ?`,
		messageID, account,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check duplicate: %w", err)
	}
	return count > 0, nil
}

func (s *Store) MarkDelivered(account, messageID string) error {
	if messageID == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO seen_messages (message_id, account, created_at) VALUES (?, ?, strftime('%s','now'))`,
		messageID, account,
	)
	if err != nil {
		return fmt.Errorf("mark delivered: %w", err)
	}
	return nil
}

func (s *Store) CleanSeen(before time.Time) error {
	_, err := s.db.Exec(`DELETE FROM seen_messages WHERE created_at < ?`, before.Unix())
	if err != nil {
		return fmt.Errorf("clean seen: %w", err)
	}
	return nil
}
