package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	awsclient "ec2manager/aws"
	"ec2manager/middleware"
	"ec2manager/models"
)

// ──────────────────────────────────────────────────────────────
// API-specific middleware (returns JSON instead of redirects)
// ──────────────────────────────────────────────────────────────

// APIAuthMW validates the session cookie and returns 401 JSON on failure.
func (h *Handler) APIAuthMW() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Cookies(middleware.SessionCookieName)
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Authentication required."})
		}
		session, err := models.GetSessionByToken(h.DB, token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Session expired."})
		}
		user, err := models.GetUserByID(h.DB, session.UserID)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "User not found."})
		}
		c.Locals(middleware.UserLocalsKey, user)
		return c.Next()
	}
}

// APIAdminOnlyMW returns 403 JSON if the user is not an admin.
func APIAdminOnlyMW(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if user == nil || user.Role != models.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Admin access required."})
	}
	return c.Next()
}

// ──────────────────────────────────────────────────────────────
// Auth API
// ──────────────────────────────────────────────────────────────

// APIMe returns the authenticated user's public info.
// GET /api/me
func (h *Handler) APIMe(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	return c.JSON(fiber.Map{
		"id":           user.ID,
		"username":     user.Username,
		"role":         user.Role,
		"totp_enabled": user.TOTPEnabled,
		"instance_id":  nullStringVal(user.InstanceID),
		"created_at":   user.CreatedAt,
	})
}

// APILogin handles PIN authentication and returns JSON.
// POST /api/login
func (h *Handler) APILogin(c *fiber.Ctx) error {
	ip := c.IP()

	if !h.RL.IsAllowed(ip) {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "Too many failed attempts. Please wait 15 minutes before trying again.",
		})
	}

	var body struct {
		Username string `json:"username"`
		PIN      string `json:"pin"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}

	username := sanitize(body.Username)
	pin := body.PIN

	fail := func(msg string) error {
		h.RL.RecordFailedAttempt(ip)
		h.logAction(nil, models.ActionLoginFail, "", fmt.Sprintf(`{"username":"%s","ip":"%s"}`, username, ip))
		h.Logger.Warn("failed login attempt", "username", username, "ip", ip)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": msg})
	}

	if username == "" || pin == "" {
		return fail("Username and PIN are required.")
	}

	user, err := models.GetUserByUsername(h.DB, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fail("Invalid username or PIN.")
		}
		h.Logger.Error("db error during login", "error", err)
		return fail("An internal error occurred. Please try again.")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PINHash), []byte(pin)); err != nil {
		return fail("Invalid username or PIN.")
	}

	h.RL.Reset(ip)

	if user.TOTPEnabled {
		challengeToken, err := generateToken()
		if err != nil {
			return fiber.ErrInternalServerError
		}
		expiresAt := time.Now().Add(5 * time.Minute)
		if err := models.CreateMFAChallenge(h.DB, user.ID, challengeToken, expiresAt); err != nil {
			return fiber.ErrInternalServerError
		}
		c.Cookie(&fiber.Cookie{
			Name:     "mfa_challenge",
			Value:    challengeToken,
			Path:     "/",
			Expires:  expiresAt,
			HTTPOnly: true,
			SameSite: "Lax",
		})
		h.Logger.Info("MFA challenge issued", "username", user.Username, "ip", ip)
		return c.JSON(fiber.Map{"mfa_required": true})
	}

	token, err := generateToken()
	if err != nil {
		return fiber.ErrInternalServerError
	}
	expiresAt := time.Now().Add(time.Duration(h.Config.SessionDuration) * time.Hour)
	if err := models.CreateSession(h.DB, user.ID, token, expiresAt); err != nil {
		return fiber.ErrInternalServerError
	}
	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	h.logAction(&user.ID, models.ActionLogin, "", fmt.Sprintf(`{"ip":"%s"}`, ip))
	h.Logger.Info("user logged in", "username", user.Username, "role", user.Role, "ip", ip)
	return c.JSON(fiber.Map{"role": string(user.Role)})
}

// APILogout invalidates the session.
// POST /api/logout
func (h *Handler) APILogout(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	token := c.Cookies(middleware.SessionCookieName)
	if token != "" {
		_ = models.DeleteSession(h.DB, token)
	}
	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-time.Hour),
		MaxAge:   -1,
		HTTPOnly: true,
	})
	if user != nil {
		h.logAction(&user.ID, models.ActionLogout, "", "")
		h.Logger.Info("user logged out", "username", user.Username)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// ──────────────────────────────────────────────────────────────
// MFA API
// ──────────────────────────────────────────────────────────────

// APIMFAVerify validates the TOTP code during login and creates a full session.
// POST /api/mfa/verify
func (h *Handler) APIMFAVerify(c *fiber.Ctx) error {
	ip := c.IP()

	if !h.RL.IsAllowed(ip) {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "Too many failed attempts. Please wait before trying again.",
		})
	}

	challengeToken := c.Cookies("mfa_challenge")
	if challengeToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No active MFA challenge."})
	}

	challenge, err := models.GetMFAChallenge(h.DB, challengeToken)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "MFA challenge expired or invalid."})
	}

	user, err := models.GetUserByID(h.DB, challenge.UserID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "User not found."})
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}

	if !totp.Validate(body.Code, user.TOTPSecret.String) {
		h.RL.RecordFailedAttempt(ip)
		h.Logger.Warn("invalid TOTP code", "user_id", user.ID, "ip", ip)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid code. Please try again."})
	}

	_ = models.DeleteMFAChallenge(h.DB, challengeToken)
	h.RL.Reset(ip)

	c.Cookie(&fiber.Cookie{
		Name:    "mfa_challenge",
		Value:   "",
		Path:    "/",
		Expires: time.Now().Add(-time.Hour),
		MaxAge:  -1,
	})

	sessionToken, err := generateToken()
	if err != nil {
		return fiber.ErrInternalServerError
	}
	expiresAt := time.Now().Add(time.Duration(h.Config.SessionDuration) * time.Hour)
	if err := models.CreateSession(h.DB, user.ID, sessionToken, expiresAt); err != nil {
		return fiber.ErrInternalServerError
	}
	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	h.logAction(&user.ID, models.ActionLogin, "", fmt.Sprintf(`{"ip":"%s","mfa":true}`, ip))
	h.Logger.Info("user logged in with MFA", "username", user.Username, "ip", ip)
	return c.JSON(fiber.Map{"role": string(user.Role)})
}

// APIMFASetupGet generates a TOTP secret and returns the QR code.
// GET /api/mfa/setup
func (h *Handler) APIMFASetupGet(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "EC2Manager",
		AccountName: user.Username,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return fiber.ErrInternalServerError
	}

	if err := models.SaveTOTPSecret(h.DB, user.ID, key.Secret()); err != nil {
		return fiber.ErrInternalServerError
	}

	img, err := key.Image(256, 256)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fiber.ErrInternalServerError
	}

	return c.JSON(fiber.Map{
		"qr_code": base64.StdEncoding.EncodeToString(buf.Bytes()),
		"secret":  key.Secret(),
	})
}

// APIMFASetupPost verifies the first TOTP code and activates MFA.
// POST /api/mfa/setup
func (h *Handler) APIMFASetupPost(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	freshUser, err := models.GetUserByID(h.DB, user.ID)
	if err != nil || !freshUser.TOTPSecret.Valid {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Setup session expired. Please start again."})
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}

	if !totp.Validate(body.Code, freshUser.TOTPSecret.String) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Incorrect code. Make sure your device time is correct and try again."})
	}

	if err := models.EnableTOTP(h.DB, user.ID); err != nil {
		return fiber.ErrInternalServerError
	}

	_ = models.DeleteUserSessions(h.DB, user.ID)
	h.Logger.Info("MFA enabled", "user", user.Username)
	return c.JSON(fiber.Map{"message": "MFA enabled successfully."})
}

// APIMFADisable verifies the TOTP code and disables MFA.
// POST /api/mfa/disable
func (h *Handler) APIMFADisable(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	freshUser, err := models.GetUserByID(h.DB, user.ID)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if !freshUser.TOTPEnabled {
		return c.JSON(fiber.Map{"message": "MFA is already disabled."})
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}

	if !totp.Validate(body.Code, freshUser.TOTPSecret.String) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Incorrect MFA code."})
	}

	if err := models.DisableTOTP(h.DB, user.ID); err != nil {
		return fiber.ErrInternalServerError
	}

	h.Logger.Info("MFA disabled by user", "user", user.Username)
	return c.JSON(fiber.Map{"message": "MFA disabled successfully."})
}

// ──────────────────────────────────────────────────────────────
// Developer (instance control) API
// ──────────────────────────────────────────────────────────────

// APIDashboard returns the developer's instance info.
// GET /api/dashboard
func (h *Handler) APIDashboard(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	result := fiber.Map{}

	if user.InstanceID.Valid && user.InstanceID.String != "" {
		instID := user.InstanceID.String

		awsInfo, err := h.EC2.DescribeInstance(c.Context(), instID)
		if err != nil {
			h.Logger.Error("describe instance failed", "instance_id", instID, "error", err)
			result["aws_error"] = fmt.Sprintf("Could not retrieve instance status: %v", err)
		} else {
			result["aws_instance"] = fiber.Map{
				"instance_id":   awsInfo.InstanceID,
				"state":         awsInfo.State,
				"public_ip":     awsInfo.PublicIP,
				"instance_type": awsInfo.InstanceType,
			}
		}

		dbInst, err := models.GetInstance(h.DB, instID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			h.Logger.Warn("failed to read instance heartbeat record", "instance_id", instID, "error", err)
		}
		if dbInst != nil {
			result["db_instance"] = fiber.Map{
				"instance_id":      dbInst.InstanceID,
				"status":           dbInst.Status,
				"last_heartbeat_at": nullTimeStr(dbInst.LastHeartbeatAt, "02 Jan 15:04 UTC"),
				"last_active_at":   nullTimeStr(dbInst.LastActiveAt, "02 Jan 15:04 UTC"),
			}
		}
	}

	return c.JSON(result)
}

// APIStartInstance starts the user's EC2 instance.
// POST /api/start-instance
func (h *Handler) APIStartInstance(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if !user.InstanceID.Valid || user.InstanceID.String == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No instance assigned to your account."})
	}
	instID := user.InstanceID.String

	if err := h.EC2.StartInstance(context.Background(), instID); err != nil {
		h.Logger.Error("StartInstance failed", "instance_id", instID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to start instance: %v", err)})
	}

	h.logAction(&user.ID, models.ActionStart, instID, `{"triggered_by":"user"}`)
	h.Logger.Info("instance started", "instance_id", instID, "user", user.Username)
	return c.JSON(fiber.Map{"message": "Instance is starting…"})
}

// APIStopInstance stops the user's EC2 instance.
// POST /api/stop-instance
func (h *Handler) APIStopInstance(c *fiber.Ctx) error {
	user := middleware.GetUser(c)
	if !user.InstanceID.Valid || user.InstanceID.String == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No instance assigned to your account."})
	}
	instID := user.InstanceID.String

	if err := h.EC2.StopInstance(context.Background(), instID); err != nil {
		h.Logger.Error("StopInstance failed", "instance_id", instID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to stop instance: %v", err)})
	}

	h.logAction(&user.ID, models.ActionStop, instID, `{"triggered_by":"user"}`)
	h.Logger.Info("instance stopped", "instance_id", instID, "user", user.Username)
	return c.JSON(fiber.Map{"message": "Instance is stopping…"})
}

// ──────────────────────────────────────────────────────────────
// Admin API
// ──────────────────────────────────────────────────────────────

// APIAdminDashboard returns all admin data.
// GET /api/admin
func (h *Handler) APIAdminDashboard(c *fiber.Ctx) error {
	users, err := models.ListUsers(h.DB)
	if err != nil {
		h.Logger.Error("list users failed", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load user list."})
	}

	instances, _ := models.ListInstances(h.DB)
	rawLogs, _ := models.ListLogs(h.DB, 100)
	enrichedLogs := h.enrichLogs(rawLogs, users)

	usersJSON := make([]fiber.Map, len(users))
	for i, u := range users {
		usersJSON[i] = fiber.Map{
			"id":                      u.ID,
			"username":                u.Username,
			"role":                    u.Role,
			"instance_id":             nullStringVal(u.InstanceID),
			"created_at":              u.CreatedAt.Format("02 Jan 2006"),
			"totp_enabled":            u.TOTPEnabled,
			"workspace_password":      nullStringVal(u.WorkspacePassword),
			"workspace_guard_password": nullStringVal(u.WorkspaceGuardPassword),
		}
	}

	instancesJSON := make([]fiber.Map, len(instances))
	for i, inst := range instances {
		instancesJSON[i] = fiber.Map{
			"instance_id":      inst.InstanceID,
			"status":           inst.Status,
			"last_heartbeat_at": nullTimeStr(inst.LastHeartbeatAt, "02 Jan 2006 15:04:05"),
			"last_active_at":   nullTimeStr(inst.LastActiveAt, "02 Jan 2006 15:04:05"),
		}
	}

	logsJSON := make([]fiber.Map, len(enrichedLogs))
	for i, l := range enrichedLogs {
		logsJSON[i] = fiber.Map{
			"timestamp":   l.Timestamp.Format("02 Jan 15:04:05"),
			"username":    l.Username,
			"action":      l.Action,
			"instance_id": nullStringVal(l.InstanceID),
			"metadata":    nullStringVal(l.Metadata),
		}
	}

	return c.JSON(fiber.Map{
		"users":     usersJSON,
		"instances": instancesJSON,
		"logs":      logsJSON,
	})
}

// APIAddUser creates a new user.
// POST /api/admin/users
func (h *Handler) APIAddUser(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		PIN      string `json:"pin"`
		Role     string `json:"role"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}

	username := sanitize(body.Username)
	role := models.Role(body.Role)
	if role != models.RoleAdmin && role != models.RoleDeveloper {
		role = models.RoleDeveloper
	}
	if username == "" || body.PIN == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Username and PIN are required."})
	}
	if len(body.PIN) < 4 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "PIN must be at least 4 characters."})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.PIN), 12)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal error."})
	}
	if _, err := models.CreateUser(h.DB, username, string(hash), role); err != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fmt.Sprintf("Failed to create user: %v", err)})
	}

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin created user", "admin", adminUser.Username, "new_user", username, "role", role)
	return c.JSON(fiber.Map{"message": fmt.Sprintf("User '%s' created successfully.", username)})
}

// APIAssignInstance assigns an EC2 instance to a user.
// POST /api/admin/users/:id/assign
func (h *Handler) APIAssignInstance(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID."})
	}

	var body struct {
		InstanceID string `json:"instance_id"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}

	instanceID := sanitize(body.InstanceID)
	if instanceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Instance ID is required."})
	}
	if err := models.UpdateUserInstance(h.DB, userID, instanceID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to assign instance: %v", err)})
	}

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin assigned instance", "admin", adminUser.Username, "user_id", userID, "instance_id", instanceID)
	return c.JSON(fiber.Map{"message": "Instance assigned successfully."})
}

// APIResetPIN resets a user's PIN hash.
// POST /api/admin/users/:id/reset-pin
func (h *Handler) APIResetPIN(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID."})
	}

	var body struct {
		NewPIN string `json:"new_pin"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}
	if len(body.NewPIN) < 4 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "New PIN must be at least 4 characters."})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPIN), 12)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal error."})
	}
	if err := models.UpdateUserPINHash(h.DB, userID, string(hash)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to reset PIN: %v", err)})
	}
	_ = models.DeleteUserSessions(h.DB, userID)

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin reset user PIN", "admin", adminUser.Username, "user_id", userID)
	return c.JSON(fiber.Map{"message": "PIN reset successfully. User sessions have been invalidated."})
}

// APIDeleteUser removes a user account.
// POST /api/admin/users/:id/delete
func (h *Handler) APIDeleteUser(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID."})
	}

	adminUser := middleware.GetUser(c)
	if adminUser.ID == userID {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "You cannot delete your own account."})
	}

	target, err := models.GetUserByID(h.DB, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found."})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to look up user."})
	}

	if err := models.DeleteUser(h.DB, userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to delete user: %v", err)})
	}

	h.Logger.Info("admin deleted user", "admin", adminUser.Username, "deleted_user", target.Username)
	return c.JSON(fiber.Map{"message": fmt.Sprintf("User '%s' deleted.", target.Username)})
}

// APIProvisionWorkspace launches a new EC2 instance for a developer.
// POST /api/admin/users/:id/provision
func (h *Handler) APIProvisionWorkspace(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID."})
	}

	target, err := models.GetUserByID(h.DB, userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found."})
	}

	cfg := h.Config
	if cfg.WorkspaceAMI == "" || cfg.WorkspaceSubnetID == "" || cfg.WorkspaceSecurityGroupID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Workspace provisioning is not configured (missing AMI, subnet, or security group)."})
	}

	var body struct {
		DevPassword   string `json:"dev_password"`
		GuardPassword string `json:"guard_password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body."})
	}
	if body.DevPassword == "" || body.GuardPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "dev_password and guard_password are required."})
	}

	adminUser := middleware.GetUser(c)
	nameTag := fmt.Sprintf("workspace-%s", target.Username)
	h.Logger.Info("provisioning workspace", "admin", adminUser.Username, "for_user", target.Username)

	userData, err := awsclient.BuildSetupScript(target.Username, body.DevPassword, body.GuardPassword)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("Failed to build setup script: %v", err)})
	}

	instanceID, err := h.EC2.LaunchInstance(c.Context(), awsclient.WorkspaceLaunchInput{
		AMIID:           cfg.WorkspaceAMI,
		InstanceType:    cfg.WorkspaceInstanceType,
		KeyName:         cfg.WorkspaceKeyName,
		SecurityGroupID: cfg.WorkspaceSecurityGroupID,
		SubnetID:        cfg.WorkspaceSubnetID,
		NameTag:         nameTag,
		UserData:        userData,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to launch instance: %v", err)})
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := h.EC2.WaitUntilRunning(waitCtx, instanceID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("Instance %s launched but did not reach running state in time.", instanceID),
		})
	}

	publicIP, err := h.EC2.AllocateAndAssociateEIP(context.Background(), instanceID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("Instance %s running but EIP assignment failed: %v", instanceID, err),
		})
	}

	if err := models.UpdateUserInstance(h.DB, userID, instanceID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("Instance %s launched at %s but DB update failed: %v", instanceID, publicIP, err),
		})
	}

	if err := models.UpdateWorkspaceCredentials(h.DB, userID, body.DevPassword, body.GuardPassword); err != nil {
		h.Logger.Error("UpdateWorkspaceCredentials failed", "user_id", userID, "error", err)
		// Non-fatal: instance is running; log and continue.
	}

	meta := fmt.Sprintf(`{"ami":"%s","instance_type":"%s","public_ip":"%s","provisioned_by":"%s"}`,
		cfg.WorkspaceAMI, cfg.WorkspaceInstanceType, publicIP, adminUser.Username)
	h.logAction(&adminUser.ID, models.ActionProvision, instanceID, meta)
	h.Logger.Info("workspace provisioned", "instance_id", instanceID, "public_ip", publicIP, "user", target.Username)

	return c.JSON(fiber.Map{
		"message":     fmt.Sprintf("Workspace provisioned for '%s': instance %s at %s", target.Username, instanceID, publicIP),
		"instance_id": instanceID,
		"public_ip":   publicIP,
	})
}

// APIResetMFA disables MFA for a user without requiring their code (admin recovery).
// POST /api/admin/users/:id/reset-mfa
func (h *Handler) APIResetMFA(c *fiber.Ctx) error {
	userID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID."})
	}

	if err := models.DisableTOTP(h.DB, userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to reset MFA: %v", err)})
	}
	_ = models.DeleteUserSessions(h.DB, userID)

	adminUser := middleware.GetUser(c)
	h.Logger.Info("admin reset MFA", "admin", adminUser.Username, "user_id", userID)
	return c.JSON(fiber.Map{"message": "MFA has been disabled for the user. Their sessions have been invalidated."})
}

// ──────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────

func nullStringVal(ns sql.NullString) interface{} {
	if !ns.Valid {
		return nil
	}
	return ns.String
}

func nullTimeStr(nt sql.NullTime, layout string) interface{} {
	if !nt.Valid {
		return nil
	}
	return nt.Time.Format(layout)
}
