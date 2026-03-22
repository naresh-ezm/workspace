package handlers

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

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

// Heartbeat receives a periodic ping from a dev EC2 instance (POST /api/heartbeat).
func (h *Handler) Heartbeat(c *fiber.Ctx) error {
	var req heartbeatRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(heartbeatResponse{OK: false, Message: "invalid JSON body"})
	}

	req.InstanceID = strings.TrimSpace(req.InstanceID)
	req.Status = strings.TrimSpace(strings.ToLower(req.Status))

	if req.InstanceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(heartbeatResponse{OK: false, Message: "instance_id is required"})
	}
	if req.Status != "active" && req.Status != "idle" {
		return c.Status(fiber.StatusBadRequest).JSON(heartbeatResponse{OK: false, Message: "status must be 'active' or 'idle'"})
	}

	// Verify the instance_id is assigned to at least one user.
	var count int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE instance_id = ?`, req.InstanceID).Scan(&count); err != nil || count == 0 {
		h.Logger.Warn("heartbeat from unknown instance", "instance_id", req.InstanceID)
		return c.JSON(heartbeatResponse{OK: false, Message: "unknown instance_id"})
	}

	status := models.StatusIdle
	updateActive := false
	if req.Status == "active" {
		status = models.StatusActive
		updateActive = true
	}

	if err := models.UpsertInstance(h.DB, req.InstanceID, status, updateActive); err != nil {
		h.Logger.Error("failed to upsert instance heartbeat", "instance_id", req.InstanceID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(heartbeatResponse{OK: false, Message: "database error"})
	}

	meta := fmt.Sprintf(`{"status":"%s"}`, req.Status)
	h.logAction(nil, models.ActionHeartbeat, req.InstanceID, meta)
	h.Logger.Debug("heartbeat received", "instance_id", req.InstanceID, "status", req.Status)

	return c.JSON(heartbeatResponse{OK: true})
}
