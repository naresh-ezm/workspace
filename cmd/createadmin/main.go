// cmd/createadmin/main.go
//
// Creates or resets an admin user directly in the SQLite database.
//
// Usage:
//
//	go run ./cmd/createadmin -username alice -pin s3cur3pin
//	go run ./cmd/createadmin                          # prompts interactively
//	DB_PATH=./prod.db go run ./cmd/createadmin -username alice -pin s3cur3pin
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	username := flag.String("username", "", "Admin username (required)")
	pin := flag.String("pin", "", "Admin PIN / password (omit to be prompted securely)")
	dbPath := flag.String("db", getEnv("DB_PATH", "app.db"), "Path to the SQLite database file")
	flag.Parse()

	// ── Username ──────────────────────────────────────────────
	if *username == "" {
		fmt.Print("Username: ")
		fmt.Scan(username)
	}
	if *username == "" {
		fatal("username is required")
	}

	// ── PIN ───────────────────────────────────────────────────
	if *pin == "" {
		*pin = promptPIN()
	}
	if len(*pin) < 4 {
		fatal("PIN must be at least 4 characters")
	}

	// ── Database ──────────────────────────────────────────────
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_foreign_keys=on", *dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fatal("open database: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(db); err != nil {
		fatal("ensure schema: %v", err)
	}

	// ── Hash PIN ──────────────────────────────────────────────
	hash, err := bcrypt.GenerateFromPassword([]byte(*pin), 12)
	if err != nil {
		fatal("hash PIN: %v", err)
	}

	// ── Upsert admin ──────────────────────────────────────────
	exists, err := userExists(db, *username)
	if err != nil {
		fatal("check user: %v", err)
	}

	if exists {
		if err := updateAdmin(db, *username, string(hash)); err != nil {
			fatal("update admin: %v", err)
		}
		fmt.Printf("✓ Admin '%s' updated (role set to admin, PIN reset).\n", *username)
	} else {
		if err := createAdmin(db, *username, string(hash)); err != nil {
			fatal("create admin: %v", err)
		}
		fmt.Printf("✓ Admin '%s' created successfully.\n", *username)
	}
}

// ──────────────────────────────────────────────────────────────
// DB helpers
// ──────────────────────────────────────────────────────────────

func ensureSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		username    TEXT    UNIQUE NOT NULL,
		pin_hash    TEXT    NOT NULL,
		role        TEXT    NOT NULL DEFAULT 'developer',
		instance_id TEXT,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	return err
}

func userExists(db *sql.DB, username string) (bool, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func createAdmin(db *sql.DB, username, pinHash string) error {
	_, err := db.Exec(
		`INSERT INTO users (username, pin_hash, role) VALUES (?, ?, 'admin')`,
		username, pinHash,
	)
	return err
}

func updateAdmin(db *sql.DB, username, pinHash string) error {
	_, err := db.Exec(
		`UPDATE users SET pin_hash = ?, role = 'admin' WHERE username = ?`,
		pinHash, username,
	)
	return err
}

// ──────────────────────────────────────────────────────────────
// IO helpers
// ──────────────────────────────────────────────────────────────

// promptPIN reads a PIN from the terminal without echoing it, then asks for
// confirmation.
func promptPIN() string {
	for {
		fmt.Print("PIN (hidden): ")
		p1, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fatal("read PIN: %v", err)
		}

		fmt.Print("Confirm PIN: ")
		p2, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fatal("read PIN confirmation: %v", err)
		}

		if string(p1) != string(p2) {
			fmt.Fprintln(os.Stderr, "PINs do not match, try again.")
			continue
		}
		if len(p1) < 4 {
			fmt.Fprintln(os.Stderr, "PIN must be at least 4 characters, try again.")
			continue
		}
		return string(p1)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
