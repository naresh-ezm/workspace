package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"ec2manager/middleware"
	"ec2manager/models"

	"golang.org/x/crypto/bcrypt"
)

type adminData struct {
	BaseData
	Users     []*models.User
	Instances []*models.Instance
	Logs      []*LogEntry
	Error     string
	Success   string
}

// AdminDashboard renders the admin control panel (GET /admin).
func (h *Handler) AdminDashboard(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	data := adminData{
		BaseData: BaseData{CurrentUser: user},
		Success:  c.Query("success"),
		Error:    c.Query("error"),
	}

	users, err := models.ListUsers(h.DB)
	if err != nil {
		h.Logger.Error("list users failed", "error", err)
		data.Error = "Failed to load user list."
	}
	data.Users = users

	instances, err := models.ListInstances(h.DB)
	if err != nil {
		h.Logger.Warn("list instances failed", "error", err)
	}
	data.Instances = instances

	rawLogs, err := models.ListLogs(h.DB, 100)
	if err != nil {
		h.Logger.Warn("list logs failed", "error", err)
	}
	data.Logs = h.enrichLogs(rawLogs, users)

	return h.render(c, "admin", data)
}

// AddUser handles user creation (POST /admin/users).
func (h *Handler) AddUser(c *fiber.Ctx) error {
	username := sanitize(c.FormValue("username"))
	pin := c.FormValue("pin")
	role := models.Role(c.FormValue("role"))

	if username == "" || pin == "" {
		return h.redirectAdmin(c, "", "Username and PIN are required.")
	}
	if role != models.RoleAdmin && role != models.RoleDeveloper {
		role = models.RoleDeveloper
	}
	if len(pin) < 4 {
		return h.redirectAdmin(c, "", "PIN must be at least 4 characters.")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pin), 12)
	if err != nil {
		h.Logger.Error("bcrypt failed", "error", err)
		return h.redirectAdmin(c, "", "Internal error – please try again.")
	}

	if _, err := models.CreateUser(h.DB, username, string(hash), role); err != nil {
		h.Logger.Error("create user failed", "username", username, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Failed to create user: %v", err))
	}

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin created user", "admin", adminUser.Username, "new_user", username, "role", role)
	return h.redirectAdmin(c, fmt.Sprintf("User '%s' created successfully.", username), "")
}

// AssignInstance sets the EC2 instance_id for a developer (POST /admin/users/:id/assign).
func (h *Handler) AssignInstance(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID")
	}

	instanceID := sanitize(c.FormValue("instance_id"))
	if instanceID == "" {
		return h.redirectAdmin(c, "", "Instance ID is required.")
	}

	if err := models.UpdateUserInstance(h.DB, userID, instanceID); err != nil {
		h.Logger.Error("assign instance failed", "user_id", userID, "instance_id", instanceID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Failed to assign instance: %v", err))
	}

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin assigned instance", "admin", adminUser.Username, "user_id", userID, "instance_id", instanceID)
	return h.redirectAdmin(c, "Instance assigned successfully.", "")
}

// ResetPIN replaces a user's PIN hash (POST /admin/users/:id/reset-pin).
func (h *Handler) ResetPIN(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID")
	}

	newPIN := c.FormValue("new_pin")
	if len(newPIN) < 4 {
		return h.redirectAdmin(c, "", "New PIN must be at least 4 characters.")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPIN), 12)
	if err != nil {
		h.Logger.Error("bcrypt failed", "error", err)
		return h.redirectAdmin(c, "", "Internal error – please try again.")
	}

	if err := models.UpdateUserPINHash(h.DB, userID, string(hash)); err != nil {
		h.Logger.Error("reset PIN failed", "user_id", userID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Failed to reset PIN: %v", err))
	}

	if err := models.DeleteUserSessions(h.DB, userID); err != nil {
		h.Logger.Warn("failed to clear sessions after PIN reset", "user_id", userID, "error", err)
	}

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin reset user PIN", "admin", adminUser.Username, "user_id", userID)
	return h.redirectAdmin(c, "PIN reset successfully. User sessions have been invalidated.", "")
}

// DeleteUser removes a user account (POST /admin/users/:id/delete).
func (h *Handler) DeleteUser(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID")
	}

	adminUser := middleware.GetUser(c)
	if adminUser.ID == userID {
		return h.redirectAdmin(c, "", "You cannot delete your own account.")
	}

	target, err := models.GetUserByID(h.DB, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return h.redirectAdmin(c, "", "User not found.")
		}
		return h.redirectAdmin(c, "", "Failed to look up user.")
	}

	if err := models.DeleteUser(h.DB, userID); err != nil {
		h.Logger.Error("delete user failed", "user_id", userID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Failed to delete user: %v", err))
	}

	h.Logger.Info("admin deleted user", "admin", adminUser.Username, "deleted_user", target.Username)
	return h.redirectAdmin(c, fmt.Sprintf("User '%s' deleted.", target.Username), "")
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

func (h *Handler) redirectAdmin(c *fiber.Ctx, success, errMsg string) error {
	q := url.Values{}
	if success != "" {
		q.Set("success", success)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	target := "/admin"
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	return c.Redirect(target, fiber.StatusSeeOther)
}

// enrichLogs joins log rows with the username from the users list.
func (h *Handler) enrichLogs(logs []*models.Log, users []*models.User) []*LogEntry {
	userMap := make(map[int64]string, len(users))
	for _, u := range users {
		userMap[u.ID] = u.Username
	}

	entries := make([]*LogEntry, 0, len(logs))
	for _, l := range logs {
		entry := &LogEntry{Log: l}
		if l.UserID.Valid {
			if name, ok := userMap[l.UserID.Int64]; ok {
				entry.Username = name
			} else {
				entry.Username = fmt.Sprintf("user#%d", l.UserID.Int64)
			}
		} else {
			entry.Username = "system"
		}
		entries = append(entries, entry)
	}
	return entries
}
