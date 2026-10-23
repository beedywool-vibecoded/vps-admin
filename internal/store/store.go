// Package store keeps the agent state in a single SQLite file inside data_dir.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

var ErrNotFound = errors.New("not found")

// Each entry runs once, in order; never edit an entry that has shipped, append a new one.
var migrations = []string{
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	CREATE TABLE devices (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		kind         TEXT NOT NULL,          -- web | app
		token_hash   BLOB UNIQUE,            -- app bearer token, NULL for web devices
		created_at   INTEGER NOT NULL,
		last_seen_at INTEGER,
		last_ip      TEXT,
		revoked_at   INTEGER
	);
	CREATE TABLE pairing_codes (
		code_hash  BLOB PRIMARY KEY,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		used_at    INTEGER
	);
	CREATE TABLE sessions (
		id_hash    BLOB PRIMARY KEY,
		device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		scope      TEXT NOT NULL,             -- full | enroll
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE TABLE passkeys (
		id           BLOB PRIMARY KEY,
		device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		credential   TEXT NOT NULL,           -- webauthn.Credential as JSON
		created_at   INTEGER NOT NULL,
		last_used_at INTEGER
	);
	CREATE TABLE audit (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		at        INTEGER NOT NULL,
		device_id TEXT,
		ip        TEXT,
		action    TEXT NOT NULL,
		target    TEXT NOT NULL DEFAULT '',
		ok        INTEGER NOT NULL,
		detail    TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX audit_at ON audit(at);`,

	`CREATE TABLE apps (
		id              TEXT PRIMARY KEY,         -- slug, also the container and project name
		repo            TEXT NOT NULL,            -- owner/name
		installation_id INTEGER NOT NULL,
		branch          TEXT NOT NULL,
		root_dir        TEXT NOT NULL DEFAULT '',
		build           TEXT NOT NULL,            -- auto | dockerfile | compose | node | static
		port            INTEGER NOT NULL DEFAULT 0, -- container port, or host port for compose
		domain          TEXT NOT NULL DEFAULT '',
		auto_deploy     INTEGER NOT NULL DEFAULT 1,
		keep            INTEGER NOT NULL DEFAULT 5,
		memory_mb       INTEGER NOT NULL DEFAULT 0,
		created_at      INTEGER NOT NULL
	);
	CREATE TABLE app_env (
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		key    TEXT NOT NULL,
		value  TEXT NOT NULL,                     -- sealed with internal/secret
		secret INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (app_id, key)
	);
	CREATE TABLE deployments (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		app_id      TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		sha         TEXT NOT NULL DEFAULT '',
		message     TEXT NOT NULL DEFAULT '',
		trigger     TEXT NOT NULL,                -- manual | push | rollback | create
		status      TEXT NOT NULL,                -- queued | building | starting | live | failed | superseded | cancelled
		build       TEXT NOT NULL DEFAULT '',     -- resolved build type
		image       TEXT NOT NULL DEFAULT '',
		container   TEXT NOT NULL DEFAULT '',
		host_port   INTEGER NOT NULL DEFAULT 0,
		error       TEXT NOT NULL DEFAULT '',
		log         TEXT NOT NULL DEFAULT '',
		created_at  INTEGER NOT NULL,
		finished_at INTEGER
	);
	CREATE INDEX deployments_app ON deployments(app_id, id);`,

	`CREATE TABLE conversations (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL,
		state      TEXT NOT NULL,                 -- idle | running | awaiting | error
		error      TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE conv_messages (
		conv_id    TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		seq        INTEGER NOT NULL,
		role       TEXT NOT NULL,
		content    TEXT NOT NULL,                 -- the API message param, as sent: never edited
		created_at INTEGER NOT NULL,
		PRIMARY KEY (conv_id, seq)
	);
	CREATE TABLE tool_calls (
		id          TEXT PRIMARY KEY,             -- tool_use id
		conv_id     TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		batch       INTEGER NOT NULL,             -- seq of the assistant message that asked
		ord         INTEGER NOT NULL,
		name        TEXT NOT NULL,
		input       TEXT NOT NULL,
		summary     TEXT NOT NULL DEFAULT '',
		kind        TEXT NOT NULL,                -- read | change | blocked
		status      TEXT NOT NULL,                -- pending | running | done | denied | blocked
		result      TEXT NOT NULL DEFAULT '',
		is_error    INTEGER NOT NULL DEFAULT 0,
		device_id   TEXT,
		created_at  INTEGER NOT NULL,
		decided_at  INTEGER
	);
	CREATE INDEX tool_calls_conv ON tool_calls(conv_id, batch, ord);`,

	`CREATE TABLE script_runs (
		id        TEXT PRIMARY KEY,
		script    TEXT NOT NULL,
		args      TEXT NOT NULL DEFAULT '',
		device_id TEXT,
		started   INTEGER NOT NULL,
		finished  INTEGER,
		exit_code INTEGER,
		error     TEXT NOT NULL DEFAULT '',
		output    TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX script_runs_script ON script_runs(script, started);`,

	// dir: an existing compose folder on the server, updated with git instead of being
	// built from a tarball. The commands override the generated Dockerfile steps.
	`ALTER TABLE apps ADD COLUMN dir TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN install_cmd TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN build_cmd TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN start_cmd TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN dockerfile TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN compose_file TEXT NOT NULL DEFAULT '';`,
	// 6: who uses each device, asked at pairing
	`ALTER TABLE devices ADD COLUMN owner TEXT NOT NULL DEFAULT '';`,
	// 7: how each app is reached
	`ALTER TABLE apps ADD COLUMN access TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN allow TEXT NOT NULL DEFAULT '';
	ALTER TABLE apps ADD COLUMN ts_port INTEGER NOT NULL DEFAULT 0;`,
	// 8: the variables can go to an .env file instead of the app's environment
	`ALTER TABLE apps ADD COLUMN env_file INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE apps ADD COLUMN env_path TEXT NOT NULL DEFAULT '';`,
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dataDir, "vps-admin.db") +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() int64 { return time.Now().Unix() }

// Setting returns the stored value for key, or def when unset.
func (s *Store) Setting(ctx context.Context, key, def string) string {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
