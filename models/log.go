package models

import (
	"database/sql"
	"time"
)

// ActionType enumerates the kinds of events we record.
type ActionType string

const (
	ActionLogin     ActionType = "LOGIN"
	ActionLogout    ActionType = "LOGOUT"
	ActionStart     ActionType = "START"
	ActionStop      ActionType = "STOP"
	ActionAutoStop  ActionType = "AUTO_STOP"
	ActionHeartbeat ActionType = "HEARTBEAT"
	ActionLoginFail ActionType = "LOGIN_FAIL"
)

// Log represents a single audit-log row.
type Log struct {
	ID         int64
	UserID     sql.NullInt64
	Action     ActionType
	InstanceID sql.NullString
	Metadata   sql.NullString
	Timestamp  time.Time
}

// CreateLog inserts a new log entry. userID, instanceID, and metadata are optional.
func CreateLog(db *sql.DB, userID *int64, action ActionType, instanceID, metadata string) error {
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	var iid sql.NullString
	if instanceID != "" {
		iid = sql.NullString{String: instanceID, Valid: true}
	}
	var meta sql.NullString
	if metadata != "" {
		meta = sql.NullString{String: metadata, Valid: true}
	}
	_, err := db.Exec(
		`INSERT INTO logs (user_id, action, instance_id, metadata, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		uid, action, iid, meta, time.Now(),
	)
	return err
}

// ListLogs returns the most recent `limit` log entries, newest first.
func ListLogs(db *sql.DB, limit int) ([]*Log, error) {
	rows, err := db.Query(
		`SELECT id, user_id, action, instance_id, metadata, timestamp
		 FROM logs ORDER BY timestamp DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*Log
	for rows.Next() {
		l := &Log{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.Action, &l.InstanceID, &l.Metadata, &l.Timestamp); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
