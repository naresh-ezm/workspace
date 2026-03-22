package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

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
func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	data := adminData{BaseData: BaseData{CurrentUser: user}}

	// Flash messages from redirects.
	data.Success = r.URL.Query().Get("success")
	data.Error = r.URL.Query().Get("error")

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

	h.render(w, h.Tmpls.Admin, data)
}

// AddUser handles user creation (POST /admin/users).
func (h *Handler) AddUser(w http.ResponseWriter, r *http.Request) {
	username := sanitize(r.FormValue("username"))
	pin := r.FormValue("pin")
	role := models.Role(r.FormValue("role"))

	if username == "" || pin == "" {
		h.redirectAdmin(w, r, "", "Username and PIN are required.")
		return
	}
	if role != models.RoleAdmin && role != models.RoleDeveloper {
		role = models.RoleDeveloper
	}
	if len(pin) < 4 {
		h.redirectAdmin(w, r, "", "PIN must be at least 4 characters.")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pin), 12)
	if err != nil {
		h.Logger.Error("bcrypt failed", "error", err)
		h.redirectAdmin(w, r, "", "Internal error – please try again.")
		return
	}

	if _, err := models.CreateUser(h.DB, username, string(hash), role); err != nil {
		h.Logger.Error("create user failed", "username", username, "error", err)
		h.redirectAdmin(w, r, "", fmt.Sprintf("Failed to create user: %v", err))
		return
	}

	adminUser := middleware.GetUser(r)
	h.Logger.Info("admin created user", "admin", adminUser.Username, "new_user", username, "role", role)
	h.redirectAdmin(w, r, fmt.Sprintf("User '%s' created successfully.", username), "")
}

// AssignInstance sets the EC2 instance_id for a developer (POST /admin/users/{id}/assign).
func (h *Handler) AssignInstance(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	instanceID := sanitize(r.FormValue("instance_id"))
	if instanceID == "" {
		h.redirectAdmin(w, r, "", "Instance ID is required.")
		return
	}

	if err := models.UpdateUserInstance(h.DB, userID, instanceID); err != nil {
		h.Logger.Error("assign instance failed", "user_id", userID, "instance_id", instanceID, "error", err)
		h.redirectAdmin(w, r, "", fmt.Sprintf("Failed to assign instance: %v", err))
		return
	}

	adminUser := middleware.GetUser(r)
	h.Logger.Info("admin assigned instance", "admin", adminUser.Username, "user_id", userID, "instance_id", instanceID)
	h.redirectAdmin(w, r, "Instance assigned successfully.", "")
}

// ResetPIN replaces a user's PIN hash (POST /admin/users/{id}/reset-pin).
func (h *Handler) ResetPIN(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	newPIN := r.FormValue("new_pin")
	if len(newPIN) < 4 {
		h.redirectAdmin(w, r, "", "New PIN must be at least 4 characters.")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPIN), 12)
	if err != nil {
		h.Logger.Error("bcrypt failed", "error", err)
		h.redirectAdmin(w, r, "", "Internal error – please try again.")
		return
	}

	if err := models.UpdateUserPINHash(h.DB, userID, string(hash)); err != nil {
		h.Logger.Error("reset PIN failed", "user_id", userID, "error", err)
		h.redirectAdmin(w, r, "", fmt.Sprintf("Failed to reset PIN: %v", err))
		return
	}

	// Invalidate all existing sessions for this user so they must re-login.
	if err := models.DeleteUserSessions(h.DB, userID); err != nil {
		h.Logger.Warn("failed to clear sessions after PIN reset", "user_id", userID, "error", err)
	}

	adminUser := middleware.GetUser(r)
	h.Logger.Info("admin reset user PIN", "admin", adminUser.Username, "user_id", userID)
	h.redirectAdmin(w, r, "PIN reset successfully. User sessions have been invalidated.", "")
}

// DeleteUser removes a user account (POST /admin/users/{id}/delete).
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Prevent self-deletion.
	adminUser := middleware.GetUser(r)
	if adminUser.ID == userID {
		h.redirectAdmin(w, r, "", "You cannot delete your own account.")
		return
	}

	target, err := models.GetUserByID(h.DB, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.redirectAdmin(w, r, "", "User not found.")
		} else {
			h.redirectAdmin(w, r, "", "Failed to look up user.")
		}
		return
	}

	if err := models.DeleteUser(h.DB, userID); err != nil {
		h.Logger.Error("delete user failed", "user_id", userID, "error", err)
		h.redirectAdmin(w, r, "", fmt.Sprintf("Failed to delete user: %v", err))
		return
	}

	h.Logger.Info("admin deleted user", "admin", adminUser.Username, "deleted_user", target.Username)
	h.redirectAdmin(w, r, fmt.Sprintf("User '%s' deleted.", target.Username), "")
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

func (h *Handler) redirectAdmin(w http.ResponseWriter, r *http.Request, success, errMsg string) {
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
	http.Redirect(w, r, target, http.StatusSeeOther)
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

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
