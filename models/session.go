package models

import (
	"database/sql"
	"time"
)

// Session represents an authenticated browser session.
type Session struct {
	ID           int64
	UserID       int64
	SessionToken string
	ExpiresAt    time.Time
}

// CreateSession inserts a new session record.
func CreateSession(db *sql.DB, userID int64, token string, expiresAt time.Time) error {
	_, err := db.Exec(
		`INSERT INTO sessions (user_id, session_token, expires_at) VALUES (?, ?, ?)`,
		userID, token, expiresAt,
	)
	return err
}

// GetSessionByToken retrieves a valid (non-expired) session.
func GetSessionByToken(db *sql.DB, token string) (*Session, error) {
	s := &Session{}
	err := db.QueryRow(
		`SELECT id, user_id, session_token, expires_at
		 FROM sessions WHERE session_token = ? AND expires_at > ?`,
		token, time.Now(),
	).Scan(&s.ID, &s.UserID, &s.SessionToken, &s.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// DeleteSession removes a session (logout).
func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE session_token = ?`, token)
	return err
}

// DeleteUserSessions removes all sessions for a user (force-logout).
func DeleteUserSessions(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// CleanExpiredSessions removes all sessions past their expiry.
func CleanExpiredSessions(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now())
	return err
}
