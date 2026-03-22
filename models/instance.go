package models

import (
	"database/sql"
	"time"
)

// InstanceStatus reflects the last-known activity state from heartbeats.
type InstanceStatus string

const (
	StatusActive  InstanceStatus = "active"
	StatusIdle    InstanceStatus = "idle"
	StatusUnknown InstanceStatus = "unknown"
)

// Instance stores heartbeat metadata for a dev EC2 desktop.
type Instance struct {
	InstanceID      string
	LastHeartbeatAt sql.NullTime
	LastActiveAt    sql.NullTime
	Status          InstanceStatus
}

// UpsertInstance creates or updates an instance heartbeat record.
// When updateActive is true, last_active_at is also refreshed.
func UpsertInstance(db *sql.DB, instanceID string, status InstanceStatus, updateActive bool) error {
	now := time.Now()
	if updateActive {
		_, err := db.Exec(`
			INSERT INTO instances (instance_id, last_heartbeat_at, last_active_at, status)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(instance_id) DO UPDATE SET
				last_heartbeat_at = excluded.last_heartbeat_at,
				last_active_at    = excluded.last_active_at,
				status            = excluded.status`,
			instanceID, now, now, status)
		return err
	}
	_, err := db.Exec(`
		INSERT INTO instances (instance_id, last_heartbeat_at, status)
		VALUES (?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			last_heartbeat_at = excluded.last_heartbeat_at,
			status            = excluded.status`,
		instanceID, now, status)
	return err
}

// GetInstance fetches a single instance record.
func GetInstance(db *sql.DB, instanceID string) (*Instance, error) {
	inst := &Instance{}
	err := db.QueryRow(
		`SELECT instance_id, last_heartbeat_at, last_active_at, status
		 FROM instances WHERE instance_id = ?`, instanceID,
	).Scan(&inst.InstanceID, &inst.LastHeartbeatAt, &inst.LastActiveAt, &inst.Status)
	if err != nil {
		return nil, err
	}
	return inst, nil
}

// ListInstances returns all tracked instances.
func ListInstances(db *sql.DB) ([]*Instance, error) {
	rows, err := db.Query(
		`SELECT instance_id, last_heartbeat_at, last_active_at, status FROM instances`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*Instance
	for rows.Next() {
		inst := &Instance{}
		if err := rows.Scan(&inst.InstanceID, &inst.LastHeartbeatAt, &inst.LastActiveAt, &inst.Status); err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}
