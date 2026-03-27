package models

import (
	"database/sql"
	"time"
)

// Role represents a user's access level.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
)

// User represents a row in the users table.
type User struct {
	ID          int64
	Username    string
	PINHash     string
	Role        Role
	InstanceID  sql.NullString
	CreatedAt   time.Time
	TOTPSecret  sql.NullString
	TOTPEnabled bool
}

// GetUserByUsername fetches a user by their username.
func GetUserByUsername(db *sql.DB, username string) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		`SELECT id, username, pin_hash, role, instance_id, created_at,
		        totp_secret, totp_enabled
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.PINHash, &u.Role, &u.InstanceID, &u.CreatedAt,
		&u.TOTPSecret, &u.TOTPEnabled)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByID fetches a user by their primary key.
func GetUserByID(db *sql.DB, id int64) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		`SELECT id, username, pin_hash, role, instance_id, created_at,
		        totp_secret, totp_enabled
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.PINHash, &u.Role, &u.InstanceID, &u.CreatedAt,
		&u.TOTPSecret, &u.TOTPEnabled)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers returns all users ordered by ID.
func ListUsers(db *sql.DB) ([]*User, error) {
	rows, err := db.Query(
		`SELECT id, username, pin_hash, role, instance_id, created_at,
		        totp_secret, totp_enabled
		 FROM users ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.PINHash, &u.Role, &u.InstanceID, &u.CreatedAt,
			&u.TOTPSecret, &u.TOTPEnabled); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// CreateUser inserts a new user record.
func CreateUser(db *sql.DB, username, pinHash string, role Role) (*User, error) {
	res, err := db.Exec(
		`INSERT INTO users (username, pin_hash, role) VALUES (?, ?, ?)`,
		username, pinHash, role,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetUserByID(db, id)
}

// UpdateUserInstance sets the EC2 instance_id for a user.
func UpdateUserInstance(db *sql.DB, userID int64, instanceID string) error {
	_, err := db.Exec(`UPDATE users SET instance_id = ? WHERE id = ?`, instanceID, userID)
	return err
}

// UpdateUserPINHash replaces the hashed PIN for a user.
func UpdateUserPINHash(db *sql.DB, userID int64, pinHash string) error {
	_, err := db.Exec(`UPDATE users SET pin_hash = ? WHERE id = ?`, pinHash, userID)
	return err
}

// DeleteUser removes a user and cascades to their sessions.
func DeleteUser(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	return err
}

// SaveTOTPSecret stores a generated secret (totp_enabled stays 0 until verified).
func SaveTOTPSecret(db *sql.DB, userID int64, secret string) error {
	_, err := db.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, secret, userID)
	return err
}

// EnableTOTP marks MFA as active after the user has verified their first code.
func EnableTOTP(db *sql.DB, userID int64) error {
	_, err := db.Exec(`UPDATE users SET totp_enabled = 1 WHERE id = ?`, userID)
	return err
}

// DisableTOTP clears the secret and disables MFA for a user.
func DisableTOTP(db *sql.DB, userID int64) error {
	_, err := db.Exec(
		`UPDATE users SET totp_secret = NULL, totp_enabled = 0 WHERE id = ?`, userID)
	return err
}
