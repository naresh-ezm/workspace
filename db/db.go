package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"ec2manager/config"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// Initialize opens the SQLite database, runs schema migrations, and seeds an
// admin user when none exists yet.
func Initialize(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", cfg.DBPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// SQLite works best with a small pool.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := ping(db); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if err := createSchema(db); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := seedAdmin(db, cfg); err != nil {
		return nil, fmt.Errorf("seed admin: %w", err)
	}

	return db, nil
}

func ping(db *sql.DB) error {
	return db.QueryRow("SELECT 1").Err()
}

func createSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			username    TEXT    UNIQUE NOT NULL,
			pin_hash    TEXT    NOT NULL,
			role        TEXT    NOT NULL DEFAULT 'developer',
			instance_id TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id       INTEGER NOT NULL,
			session_token TEXT    UNIQUE NOT NULL,
			expires_at    DATETIME NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS logs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id     INTEGER,
			action      TEXT    NOT NULL,
			instance_id TEXT,
			metadata    TEXT,
			timestamp   DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS instances (
			instance_id       TEXT PRIMARY KEY,
			last_heartbeat_at DATETIME,
			last_active_at    DATETIME,
			status            TEXT DEFAULT 'unknown'
		)`,
		// Indexes for frequent query patterns.
		`CREATE INDEX IF NOT EXISTS idx_sessions_token   ON sessions(session_token)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user    ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_timestamp   ON logs(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_user        ON logs(user_id)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec statement: %w", err)
		}
	}
	return nil
}

// seedAdmin creates the first admin account when the users table is empty.
func seedAdmin(db *sql.DB, cfg *config.Config) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPIN), 12)
	if err != nil {
		return err
	}

	if _, err := db.Exec(
		`INSERT INTO users (username, pin_hash, role) VALUES (?, ?, 'admin')`,
		cfg.AdminUsername, string(hash),
	); err != nil {
		return err
	}

	slog.Warn("Created default admin user – change the PIN immediately",
		"username", cfg.AdminUsername)
	return nil
}
