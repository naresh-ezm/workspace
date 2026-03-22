package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ec2manager/models"
)

type heartbeatRequest struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"` // "active" | "idle"
}

type heartbeatResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Heartbeat receives a periodic ping from a dev EC2 instance and updates its
// activity record (POST /api/heartbeat).
//
// This endpoint is intentionally unauthenticated so that instances can call it
// without managing credentials.  The instance_id is validated to exist in the
// users table before any write occurs.
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(heartbeatResponse{OK: false, Message: "invalid JSON body"})
		return
	}

	req.InstanceID = strings.TrimSpace(req.InstanceID)
	req.Status = strings.TrimSpace(strings.ToLower(req.Status))

	if req.InstanceID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(heartbeatResponse{OK: false, Message: "instance_id is required"})
		return
	}
	if req.Status != "active" && req.Status != "idle" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(heartbeatResponse{OK: false, Message: "status must be 'active' or 'idle'"})
		return
	}

	// Verify the instance_id is known (assigned to at least one user).
	var count int
	err := h.DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE instance_id = ?`, req.InstanceID,
	).Scan(&count)
	if err != nil || count == 0 {
		h.Logger.Warn("heartbeat from unknown instance", "instance_id", req.InstanceID)
		// Return 200 so misconfigured instances don't flood error logs.
		_ = json.NewEncoder(w).Encode(heartbeatResponse{OK: false, Message: "unknown instance_id"})
		return
	}

	// Persist heartbeat.
	status := models.StatusIdle
	updateActive := false
	if req.Status == "active" {
		status = models.StatusActive
		updateActive = true
	}

	if err := models.UpsertInstance(h.DB, req.InstanceID, status, updateActive); err != nil {
		h.Logger.Error("failed to upsert instance heartbeat", "instance_id", req.InstanceID, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(heartbeatResponse{OK: false, Message: "database error"})
		return
	}

	meta := fmt.Sprintf(`{"status":"%s"}`, req.Status)
	h.logAction(nil, models.ActionHeartbeat, req.InstanceID, meta)
	h.Logger.Debug("heartbeat received", "instance_id", req.InstanceID, "status", req.Status)

	_ = json.NewEncoder(w).Encode(heartbeatResponse{OK: true})
}
