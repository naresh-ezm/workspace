package models

import (
	"database/sql"
	"time"
)

// MFAChallenge is the short-lived bridge record between PIN auth and TOTP
// verification. It is created when a user with MFA enabled passes the PIN
// check, and deleted immediately after the TOTP code is accepted.
type MFAChallenge struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

// CreateMFAChallenge inserts a new challenge record.
func CreateMFAChallenge(db *sql.DB, userID int64, token string, expiresAt time.Time) error {
	_, err := db.Exec(
		`INSERT INTO mfa_challenges (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt,
	)
	return err
}

// GetMFAChallenge returns the challenge for token only if it has not expired.
func GetMFAChallenge(db *sql.DB, token string) (*MFAChallenge, error) {
	ch := &MFAChallenge{}
	err := db.QueryRow(
		`SELECT token, user_id, expires_at FROM mfa_challenges
		 WHERE token = ? AND expires_at > ?`, token, time.Now(),
	).Scan(&ch.Token, &ch.UserID, &ch.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// DeleteMFAChallenge removes a challenge by token.
func DeleteMFAChallenge(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM mfa_challenges WHERE token = ?`, token)
	return err
}

// CleanExpiredChallenges removes all expired MFA challenge rows.
func CleanExpiredChallenges(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM mfa_challenges WHERE expires_at <= ?`, time.Now())
	return err
}
