package handlers

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	awsclient "ec2manager/aws"
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

// ProvisionWorkspace launches a new EC2 instance from the configured AMI,
// waits for it to reach the running state, assigns an Elastic IP, and links
// the instance to the target developer in the database.
// POST /admin/users/:id/provision
func (h *Handler) ProvisionWorkspace(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID")
	}

	target, err := models.GetUserByID(h.DB, userID)
	if err != nil {
		return h.redirectAdmin(c, "", "User not found.")
	}

	cfg := h.Config
	if cfg.WorkspaceAMI == "" || cfg.WorkspaceSubnetID == "" || cfg.WorkspaceSecurityGroupID == "" {
		return h.redirectAdmin(c, "", "Workspace provisioning is not configured (missing WORKSPACE_AMI, WORKSPACE_SUBNET_ID, or WORKSPACE_SECURITY_GROUP_ID).")
	}

	adminUser := middleware.GetUser(c)
	nameTag := fmt.Sprintf("workspace-%s", target.Username)

	h.Logger.Info("provisioning workspace", "admin", adminUser.Username, "for_user", target.Username)

	// 1. Launch the instance.
	instanceID, err := h.EC2.LaunchInstance(c.Context(), awsclient.WorkspaceLaunchInput{
		AMIID:           cfg.WorkspaceAMI,
		InstanceType:    cfg.WorkspaceInstanceType,
		KeyName:         cfg.WorkspaceKeyName,
		SecurityGroupID: cfg.WorkspaceSecurityGroupID,
		SubnetID:        cfg.WorkspaceSubnetID,
		NameTag:         nameTag,
	})
	if err != nil {
		h.Logger.Error("LaunchInstance failed", "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Failed to launch instance: %v", err))
	}

	h.Logger.Info("instance launched, waiting for running state", "instance_id", instanceID)

	// 2. Wait until running (up to 6 minutes).
	waitCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := h.EC2.WaitUntilRunning(waitCtx, instanceID); err != nil {
		h.Logger.Error("WaitUntilRunning failed", "instance_id", instanceID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Instance %s launched but did not reach running state in time. Assign it manually once ready.", instanceID))
	}

	// 3. Allocate and associate an Elastic IP.
	publicIP, err := h.EC2.AllocateAndAssociateEIP(context.Background(), instanceID)
	if err != nil {
		h.Logger.Error("AllocateAndAssociateEIP failed", "instance_id", instanceID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Instance %s is running but EIP assignment failed: %v", instanceID, err))
	}

	// 4. Persist to the database.
	if err := models.UpdateUserInstance(h.DB, userID, instanceID); err != nil {
		h.Logger.Error("UpdateUserInstance failed", "user_id", userID, "instance_id", instanceID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Instance %s launched at %s but DB update failed: %v — assign it manually.", instanceID, publicIP, err))
	}

	meta := fmt.Sprintf(`{"ami":"%s","instance_type":"%s","public_ip":"%s","provisioned_by":"%s"}`,
		cfg.WorkspaceAMI, cfg.WorkspaceInstanceType, publicIP, adminUser.Username)
	h.logAction(&adminUser.ID, models.ActionProvision, instanceID, meta)
	h.Logger.Info("workspace provisioned", "instance_id", instanceID, "public_ip", publicIP, "user", target.Username)

	return h.redirectAdmin(c,
		fmt.Sprintf("Workspace provisioned for '%s': instance %s at %s", target.Username, instanceID, publicIP), "")
}

// ResetMFA disables MFA for a user without requiring their TOTP code.
// Used by admins for account recovery when a user loses their authenticator.
// POST /admin/users/:id/reset-mfa
func (h *Handler) ResetMFA(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID")
	}

	if err := models.DisableTOTP(h.DB, userID); err != nil {
		h.Logger.Error("reset MFA failed", "user_id", userID, "error", err)
		return h.redirectAdmin(c, "", fmt.Sprintf("Failed to reset MFA: %v", err))
	}

	if err := models.DeleteUserSessions(h.DB, userID); err != nil {
		h.Logger.Warn("failed to clear sessions after MFA reset", "user_id", userID, "error", err)
	}

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin reset MFA", "admin", adminUser.Username, "user_id", userID)
	return h.redirectAdmin(c, "MFA has been disabled for the user. Their sessions have been invalidated.", "")
}

// AppLogs returns the last 200 lines of the application log file as JSON.
// GET /admin/app-logs
func (h *Handler) AppLogs(c *fiber.Ctx) error {
	lines, err := tailFile(h.Config.LogFile, 200)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("could not read log file: %v", err),
		})
	}
	return c.JSON(fiber.Map{"lines": lines})
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

// tailFile reads up to n last lines from the file at path.
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

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
