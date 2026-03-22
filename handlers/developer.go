package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/gofiber/fiber/v2"

	awsclient "ec2manager/aws"
	"ec2manager/middleware"
	"ec2manager/models"
)

type dashboardData struct {
	BaseData
	AWSInstance *awsclient.InstanceInfo
	DBInstance  *models.Instance
	Error       string
	Success     string
}

// Dashboard renders the developer's instance control panel (GET /dashboard).
func (h *Handler) Dashboard(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	data := dashboardData{
		BaseData: BaseData{CurrentUser: user},
		Success:  c.Query("success"),
		Error:    c.Query("error"),
	}

	if !user.InstanceID.Valid || user.InstanceID.String == "" {
		data.Error = "No EC2 instance has been assigned to your account. Contact an administrator."
		return h.render(c, "dashboard", data)
	}

	instID := user.InstanceID.String

	awsInfo, err := h.EC2.DescribeInstance(c.Context(), instID)
	if err != nil {
		h.Logger.Error("describe instance failed", "instance_id", instID, "error", err)
		if data.Error == "" {
			data.Error = fmt.Sprintf("Could not retrieve instance status: %v", err)
		}
	} else {
		data.AWSInstance = awsInfo
	}

	dbInst, err := models.GetInstance(h.DB, instID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		h.Logger.Warn("failed to read instance heartbeat record", "instance_id", instID, "error", err)
	}
	data.DBInstance = dbInst

	return h.render(c, "dashboard", data)
}

// StartInstance calls EC2 StartInstances (POST /start-instance).
func (h *Handler) StartInstance(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	instID, ok := h.requireInstance(c, user)
	if !ok {
		return nil
	}

	if err := h.EC2.StartInstance(context.Background(), instID); err != nil {
		h.Logger.Error("StartInstance failed", "instance_id", instID, "error", err)
		return h.redirectDashboard(c, "", fmt.Sprintf("Failed to start instance: %v", err))
	}

	h.logAction(&user.ID, models.ActionStart, instID, `{"triggered_by":"user"}`)
	h.Logger.Info("instance started", "instance_id", instID, "user", user.Username)
	return h.redirectDashboard(c, "Instance is starting… it may take a minute to become fully available.", "")
}

// StopInstance calls EC2 StopInstances (POST /stop-instance).
func (h *Handler) StopInstance(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	instID, ok := h.requireInstance(c, user)
	if !ok {
		return nil
	}

	if err := h.EC2.StopInstance(context.Background(), instID); err != nil {
		h.Logger.Error("StopInstance failed", "instance_id", instID, "error", err)
		return h.redirectDashboard(c, "", fmt.Sprintf("Failed to stop instance: %v", err))
	}

	h.logAction(&user.ID, models.ActionStop, instID, `{"triggered_by":"user"}`)
	h.Logger.Info("instance stopped", "instance_id", instID, "user", user.Username)
	return h.redirectDashboard(c, "Instance is stopping…", "")
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

func (h *Handler) requireInstance(c *fiber.Ctx, user *models.User) (string, bool) {
	if !user.InstanceID.Valid || user.InstanceID.String == "" {
		_ = c.Status(fiber.StatusBadRequest).SendString("No instance assigned to your account")
		return "", false
	}
	return user.InstanceID.String, true
}

func (h *Handler) redirectDashboard(c *fiber.Ctx, success, errMsg string) error {
	q := url.Values{}
	if success != "" {
		q.Set("success", success)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	target := "/dashboard"
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	return c.Redirect(target, fiber.StatusSeeOther)
}
